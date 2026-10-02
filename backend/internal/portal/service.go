package portal

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/pagination"
)

type RowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type Service struct {
	DB      RowQuerier
	Pepper  []byte
	Random  io.Reader
	Now     func() time.Time
	Objects MultipartStore
	Bucket  string
}

type MultipartStore interface {
	BeginMultipart(context.Context, string, string) (string, error)
	AbortMultipart(context.Context, string, string) error
	SignUploadParts(context.Context, string, string, []int, time.Duration) ([]objectstore.PartURL, error)
	SignRead(context.Context, objectstore.Object, time.Duration) (objectstore.ReadURL, error)
	SignDownload(context.Context, objectstore.Object, string, time.Duration) (objectstore.ReadURL, error)
}

type ExchangeRequest struct {
	ShareID uuid.UUID `json:"share_id"`
	Secret  string    `json:"secret"`
	Locale  string    `json:"locale"`
}

type SessionInfo struct {
	ID             uuid.UUID       `json:"id"`
	ExpiresAt      time.Time       `json:"expires_at"`
	Capabilities   json.RawMessage `json:"capabilities"`
	AllowedPresets json.RawMessage `json:"allowed_presets"`
	CSRFToken      string          `json:"csrf_token"`
	Token          string          `json:"-"`
}

type Identity struct {
	SessionID, TenantID, ShareID, CollectionID uuid.UUID
	LinkVersion                                int64
	Locale, Moderation                         string
	Permissions, Limits, AllowedPresets        json.RawMessage
}

type Collection struct {
	Name, Description string          `json:"name"`
	Revision          string          `json:"revision"`
	Cover             json.RawMessage `json:"cover,omitempty"`
	Capabilities      json.RawMessage `json:"capabilities"`
	AllowedPresets    json.RawMessage `json:"allowed_presets"`
	Sections          json.RawMessage `json:"sections"`
	Empty             bool            `json:"empty"`
}

type GuestUploadBegin struct {
	Filename      string  `json:"filename"`
	Bytes         int64   `json:"bytes"`
	Format        string  `json:"format"`
	AssumeProfile *string `json:"assume_profile,omitempty"`
	SHA256        *string `json:"sha256,omitempty"`
}

type GuestUpload struct {
	ID                      uuid.UUID `json:"id"`
	State                   string    `json:"state"`
	Revision                string    `json:"revision"`
	Filename                string    `json:"filename"`
	ExpectedSize            int64     `json:"expected_size"`
	ActualSize              *int64    `json:"actual_size,omitempty"`
	PartBytes               int64     `json:"part_bytes"`
	PartsTotal              int       `json:"parts_total"`
	ExpiresAt               time.Time `json:"expires_at"`
	projectID, collectionID uuid.UUID
	objectKey, mediaType    string
	multipartID             *string
}

type GuestPartRequest struct {
	PartNumbers []int `json:"part_numbers"`
}

type GuestPartURLs struct {
	UploadID uuid.UUID             `json:"upload_id"`
	Items    []objectstore.PartURL `json:"items"`
}

type GuestComplete struct {
	Parts        []objectstore.UploadedPart `json:"parts"`
	ExpectedSize int64                      `json:"expected_size"`
}

type GuestAccepted struct {
	JobID  uuid.UUID `json:"job_id"`
	Status string    `json:"status"`
}

type GuestItem struct {
	ID              uuid.UUID       `json:"id"`
	Width           int             `json:"width"`
	Height          int             `json:"height"`
	Title           string          `json:"title"`
	SectionID       *uuid.UUID      `json:"section_id,omitempty"`
	ThumbnailStatus string          `json:"thumbnail_status"`
	MediaVariants   json.RawMessage `json:"media_variants"`
	position        int64
}

type MediaRequest struct {
	ItemIDs  []uuid.UUID `json:"item_ids"`
	Variants []string    `json:"variants"`
}

type MediaLink struct {
	ItemID    uuid.UUID `json:"item_id"`
	Variant   string    `json:"variant"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	MediaType string    `json:"media_type"`
}

type MediaLinks struct {
	Items []MediaLink `json:"items"`
}

type SelectionFilter struct {
	SectionID uuid.UUID `json:"section_id"`
}

type SelectionRequest struct {
	CollectionRevision string           `json:"collection_revision"`
	Scope              string           `json:"scope"`
	ItemIDs            []uuid.UUID      `json:"item_ids,omitempty"`
	Filter             *SelectionFilter `json:"filter,omitempty"`
}

type SnapshotRef struct {
	ID        uuid.UUID `json:"id"`
	Digest    string    `json:"digest"`
	ItemCount int64     `json:"item_count"`
	ExpiresAt time.Time `json:"expires_at"`
}

type DownloadRequest struct {
	SelectionSnapshotID uuid.UUID `json:"selection_snapshot_id"`
	PresetID            uuid.UUID `json:"preset_id"`
}

type DownloadAccepted struct {
	DownloadJobID uuid.UUID `json:"download_job_id"`
	Status        string    `json:"status"`
}

type DownloadVolume struct {
	Number       int    `json:"number"`
	Status       string `json:"status"`
	Bytes        int64  `json:"bytes"`
	DeliveryKind string `json:"delivery_kind"`
}

type DownloadStatus struct {
	ID           uuid.UUID        `json:"id"`
	Status       string           `json:"status"`
	FilesDone    int64            `json:"files_done"`
	FilesTotal   int64            `json:"files_total"`
	BytesWritten int64            `json:"bytes_written"`
	Volumes      []DownloadVolume `json:"volumes"`
	ExpiresAt    *time.Time       `json:"expires_at,omitempty"`
	Error        json.RawMessage  `json:"error,omitempty"`
}

type DownloadIntentRequest struct {
	Volume int `json:"volume"`
}
type SingleDownloadRequest struct {
	PresetID uuid.UUID `json:"preset_id"`
}
type DownloadIntent struct {
	Token     string    `json:"intent_token"`
	ExpiresAt time.Time `json:"expires_at"`
}
type DownloadRedeemRequest struct {
	Token string `json:"token"`
}
type DownloadLink struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	Filename  string    `json:"filename"`
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"sha256"`
}
type PortalEvent struct {
	Cursor    string          `json:"cursor"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}
type EventPage struct {
	Items      []PortalEvent `json:"items"`
	NextCursor string        `json:"next_cursor"`
}

func (s Service) Events(ctx context.Context, token, afterRaw string, limit int) (EventPage, error) {
	out := EventPage{Items: []PortalEvent{}, NextCursor: afterRaw}
	if token == "" || limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var after int64
	if afterRaw != "" {
		value, err := strconv.ParseInt(afterRaw, 10, 64)
		if err != nil || value < 0 {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		after = value
	}
	rows, err := s.DB.Query(ctx, `SELECT outcome,cursor,type,payload,created_at FROM photo.list_portal_events($1,$2,$3)`, auth.MAC(s.Pepper, "portal-session", token), after, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var outcome string
		var cursor *int64
		var kind *string
		var payload []byte
		var created *time.Time
		if err = rows.Scan(&outcome, &cursor, &kind, &payload, &created); err != nil {
			return out, err
		}
		switch outcome {
		case "unauthorized":
			return EventPage{}, domain.NewError("INVALID_GUEST_SESSION", 401)
		case "expired":
			return EventPage{}, domain.NewError("EVENTS_EXPIRED", 410)
		case "invalid":
			return EventPage{}, domain.NewError("VALIDATION_FAILED", 422)
		case "ok":
		default:
			return EventPage{}, errors.New("invalid portal event outcome")
		}
		if cursor != nil {
			if kind == nil || created == nil {
				return EventPage{}, errors.New("incomplete portal event")
			}
			item := PortalEvent{Cursor: strconv.FormatInt(*cursor, 10), Type: *kind, Payload: payload, CreatedAt: *created}
			out.Items = append(out.Items, item)
			out.NextCursor = item.Cursor
		}
	}
	return out, rows.Err()
}

func normalizeSelection(request SelectionRequest) (SelectionRequest, int64, *uuid.UUID, error) {
	request.Scope = strings.ToLower(strings.TrimSpace(request.Scope))
	revision, err := strconv.ParseInt(request.CollectionRevision, 10, 64)
	if err != nil || revision < 1 || revision > 9007199254740991 {
		return request, 0, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	request.CollectionRevision = strconv.FormatInt(revision, 10)
	var sectionID *uuid.UUID
	switch request.Scope {
	case "selected":
		if len(request.ItemIDs) < 1 || len(request.ItemIDs) > 5000 || request.Filter != nil {
			return request, 0, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
		sort.Slice(request.ItemIDs, func(i, j int) bool { return request.ItemIDs[i].String() < request.ItemIDs[j].String() })
		for index, id := range request.ItemIDs {
			if id == uuid.Nil || (index > 0 && id == request.ItemIDs[index-1]) {
				return request, 0, nil, domain.NewError("VALIDATION_FAILED", 422)
			}
		}
	case "all":
		if len(request.ItemIDs) != 0 || request.Filter != nil {
			return request, 0, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
		request.ItemIDs = nil
	case "filter":
		if len(request.ItemIDs) != 0 || request.Filter == nil || request.Filter.SectionID == uuid.Nil {
			return request, 0, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
		request.ItemIDs = nil
		sectionID = &request.Filter.SectionID
	default:
		return request, 0, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	return request, revision, sectionID, nil
}

func normalizeMediaRequest(request MediaRequest) (MediaRequest, error) {
	if len(request.ItemIDs) < 1 || len(request.ItemIDs) > 100 || len(request.Variants) < 1 || len(request.Variants) > 4 {
		return request, domain.NewError("VALIDATION_FAILED", 422)
	}
	itemsSeen := map[uuid.UUID]bool{}
	for _, id := range request.ItemIDs {
		if id == uuid.Nil || itemsSeen[id] {
			return request, domain.NewError("VALIDATION_FAILED", 422)
		}
		itemsSeen[id] = true
	}
	variantsSeen := map[string]bool{}
	for index, variant := range request.Variants {
		variant = strings.ToLower(strings.TrimSpace(variant))
		request.Variants[index] = variant
		if variantsSeen[variant] || (variant != "thumb320" && variant != "thumb640" && variant != "preview1600" && variant != "preview2560") {
			return request, domain.NewError("VALIDATION_FAILED", 422)
		}
		variantsSeen[variant] = true
	}
	return request, nil
}

func validGuestCompletion(request GuestComplete, upload GuestUpload) bool {
	if upload.State != "uploading" || upload.multipartID == nil || request.ExpectedSize != upload.ExpectedSize || len(request.Parts) != upload.PartsTotal || len(request.Parts) < 1 || len(request.Parts) > objectstore.MaxUploadParts {
		return false
	}
	for index, part := range request.Parts {
		if part.PartNumber != index+1 || len(part.ETag) < 1 || len(part.ETag) > 256 || strings.ContainsAny(part.ETag, "\r\n") {
			return false
		}
	}
	return true
}

var guestFormats = map[string]string{
	"jpeg": "image/jpeg", "jpg": "image/jpeg", "png": "image/png", "tiff": "image/tiff", "tif": "image/tiff",
	"dng": "image/x-adobe-dng", "cr2": "image/x-canon-cr2", "nef": "image/x-nikon-nef", "arw": "image/x-sony-arw",
	"raf": "image/x-fuji-raf", "orf": "image/x-olympus-orf", "rw2": "image/x-panasonic-rw2",
}

func normalizeGuestUpload(request GuestUploadBegin) (GuestUploadBegin, string, error) {
	request.Filename = strings.TrimSpace(request.Filename)
	request.Format = strings.ToLower(strings.TrimSpace(request.Format))
	media, ok := guestFormats[request.Format]
	if !ok || request.Filename == "" || len([]rune(request.Filename)) > 255 || strings.ContainsAny(request.Filename, "\x00/\\") || request.Bytes < 1 || request.Bytes > objectstore.MaxUploadBytes {
		return request, "", domain.NewError("VALIDATION_FAILED", 422)
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(request.Filename)), ".")
	if ext != request.Format && !(request.Format == "jpeg" && ext == "jpg") && !(request.Format == "jpg" && ext == "jpeg") && !(request.Format == "tiff" && ext == "tif") && !(request.Format == "tif" && ext == "tiff") {
		return request, "", domain.NewError("VALIDATION_FAILED", 422)
	}
	if request.AssumeProfile != nil {
		profile := strings.ToLower(strings.TrimSpace(*request.AssumeProfile))
		switch profile {
		case "srgb", "srgb_v1":
			profile = "srgb_v1"
		case "linear_srgb", "linear_srgb_v1":
			profile = "linear_srgb_v1"
		case "work_rec2020_gamma24", "work_rec2020_gamma24_v1":
			profile = "work_rec2020_gamma24_v1"
		default:
			return request, "", domain.NewError("VALIDATION_FAILED", 422)
		}
		request.AssumeProfile = &profile
	}
	if request.SHA256 != nil {
		hash := strings.ToLower(strings.TrimSpace(*request.SHA256))
		if len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
			return request, "", domain.NewError("VALIDATION_FAILED", 422)
		}
		request.SHA256 = &hash
	}
	return request, media, nil
}

func scanGuestUpload(row pgx.Row, outcome *string) (GuestUpload, error) {
	var out GuestUpload
	var id, projectID, collectionID *uuid.UUID
	var state, filename, objectKey, multipartID, mediaType *string
	var revision, expectedSize, actualSize *int64
	var expiresAt *time.Time
	args := []any{&id, &projectID, &collectionID, &state, &revision, &filename, &expectedSize, &actualSize, &expiresAt, &objectKey, &multipartID, &mediaType}
	if outcome != nil {
		args = append([]any{outcome}, args...)
	}
	err := row.Scan(args...)
	if err == nil && id != nil {
		if projectID == nil || collectionID == nil || state == nil || revision == nil || filename == nil || expectedSize == nil || expiresAt == nil || objectKey == nil || mediaType == nil {
			return GuestUpload{}, errors.New("incomplete portal upload row")
		}
		out.ID, out.projectID, out.collectionID, out.State = *id, *projectID, *collectionID, *state
		out.Revision, out.Filename, out.ExpectedSize, out.ExpiresAt = strconv.FormatInt(*revision, 10), *filename, *expectedSize, *expiresAt
		out.ActualSize, out.objectKey, out.multipartID, out.mediaType = actualSize, *objectKey, multipartID, *mediaType
		out.PartBytes = objectstore.PartBytes
		out.PartsTotal = int((out.ExpectedSize + objectstore.PartBytes - 1) / objectstore.PartBytes)
	}
	return out, err
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Service) random() io.Reader {
	if s.Random != nil {
		return s.Random
	}
	return rand.Reader
}

func normalizeExchange(request ExchangeRequest) (ExchangeRequest, error) {
	request.Secret = strings.TrimSpace(request.Secret)
	request.Locale = strings.ToLower(strings.TrimSpace(request.Locale))
	if request.ShareID == uuid.Nil || len(request.Secret) < 32 || len(request.Secret) > 256 || (request.Locale != "ru" && request.Locale != "bg" && request.Locale != "en") {
		return request, domain.NewError("INVALID_SHARE", 401)
	}
	return request, nil
}

func (s Service) Exchange(ctx context.Context, raw ExchangeRequest) (SessionInfo, error) {
	var out SessionInfo
	request, err := normalizeExchange(raw)
	if err != nil {
		return out, err
	}
	if s.DB == nil || len(s.Pepper) < 32 {
		return out, errors.New("portal dependencies unavailable")
	}
	token, err := auth.NewSecret(s.random())
	if err != nil {
		return out, err
	}
	csrf, err := auth.NewSecret(s.random())
	if err != nil {
		return out, err
	}
	expires := s.now().Add(24 * time.Hour)
	secretHash := auth.MAC(s.Pepper, "share-secret", request.ShareID.String(), request.Secret)
	sessionHash := auth.MAC(s.Pepper, "portal-session", token)
	csrfHash := auth.MAC(s.Pepper, "portal-csrf", token, csrf)
	err = s.DB.QueryRow(ctx, `SELECT session_id,expires_at,permissions,allowed_presets
		FROM photo.exchange_share_session($1,$2,$3,$4,$5,$6)`, request.ShareID, secretHash, sessionHash, csrfHash, request.Locale, expires).Scan(
		&out.ID, &out.ExpiresAt, &out.Capabilities, &out.AllowedPresets,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionInfo{}, domain.NewError("INVALID_SHARE", 401)
	}
	if err != nil {
		return SessionInfo{}, err
	}
	out.Token, out.CSRFToken = token, csrf
	return out, nil
}

func (s Service) Authorize(ctx context.Context, token, csrf, permission string, mutate bool) (Identity, error) {
	var out Identity
	if s.DB == nil || len(s.Pepper) < 32 || token == "" || (mutate && csrf == "") {
		return out, domain.NewError("INVALID_GUEST_SESSION", 401)
	}
	var csrfHash any
	if mutate {
		csrfHash = auth.MAC(s.Pepper, "portal-csrf", token, csrf)
	}
	err := s.DB.QueryRow(ctx, `SELECT session_id,tenant_id,share_id,collection_id,link_version,locale,permissions,limits,moderation,allowed_presets
		FROM photo.authorize_portal_session($1,$2,$3)`, auth.MAC(s.Pepper, "portal-session", token), csrfHash, permission).Scan(
		&out.SessionID, &out.TenantID, &out.ShareID, &out.CollectionID, &out.LinkVersion, &out.Locale, &out.Permissions, &out.Limits, &out.Moderation, &out.AllowedPresets,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, domain.NewError("INVALID_GUEST_SESSION", 401)
	}
	return out, err
}

func (s Service) Collection(ctx context.Context, token string) (Collection, error) {
	var out Collection
	if s.DB == nil || len(s.Pepper) < 32 || token == "" {
		return out, domain.NewError("INVALID_GUEST_SESSION", 401)
	}
	var revision int64
	err := s.DB.QueryRow(ctx, `SELECT name,description,revision,cover,capabilities,allowed_presets,sections,empty
		FROM photo.get_portal_collection($1)`, auth.MAC(s.Pepper, "portal-session", token)).Scan(
		&out.Name, &out.Description, &revision, &out.Cover, &out.Capabilities, &out.AllowedPresets, &out.Sections, &out.Empty,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Collection{}, domain.NewError("INVALID_GUEST_SESSION", 401)
	}
	if err != nil {
		return Collection{}, err
	}
	out.Revision = strconv.FormatInt(revision, 10)
	return out, nil
}

func (s Service) Items(ctx context.Context, token string, sectionID *uuid.UUID, pageSize int, rawCursor string) (pagination.Page[GuestItem], error) {
	out := pagination.Page[GuestItem]{Items: []GuestItem{}}
	if pageSize < 1 || pageSize > 100 || (sectionID != nil && *sectionID == uuid.Nil) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	identity, err := s.Authorize(ctx, token, "", "view", false)
	if err != nil {
		return out, err
	}
	collection, err := s.Collection(ctx, token)
	if err != nil {
		return out, err
	}
	filterHash, err := canonical.Hash(struct {
		SectionID *uuid.UUID `json:"section_id"`
	}{sectionID})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: identity.TenantID.String(), Principal: identity.SessionID.String(), FilterHash: filterHash, Sort: "portal-items:position,id", Revision: collection.Revision}
	var afterPosition *int64
	var afterID *uuid.UUID
	codec := pagination.Codec{Key: auth.MAC(s.Pepper, "portal-cursor")}
	if rawCursor != "" {
		cursor, decodeErr := codec.Decode(rawCursor, scope)
		if decodeErr != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		if cursor.Revision != collection.Revision {
			return out, domain.NewError("SNAPSHOT_STALE", 409)
		}
		position, parseErr := strconv.ParseInt(cursor.LastPosition, 10, 64)
		id, idErr := uuid.Parse(cursor.LastID)
		if parseErr != nil || idErr != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterPosition, afterID = &position, &id
	}
	rows, err := s.DB.Query(ctx, `SELECT collection_revision,item_id,section_id,position,width,height,title,thumbnail_status,media_variants
		FROM photo.list_portal_items($1,$2,$3,$4,$5)`, auth.MAC(s.Pepper, "portal-session", token), sectionID, afterPosition, afterID, pageSize+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item GuestItem
		var revision int64
		var width, height *int
		if err = rows.Scan(&revision, &item.ID, &item.SectionID, &item.position, &width, &height, &item.Title, &item.ThumbnailStatus, &item.MediaVariants); err != nil {
			return out, err
		}
		if strconv.FormatInt(revision, 10) != collection.Revision || width == nil || height == nil || *width < 1 || *height < 1 {
			return out, errors.New("invalid portal item projection")
		}
		item.Width, item.Height = *width, *height
		out.Items = append(out.Items, item)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	out.Revision = collection.Revision
	if len(out.Items) > pageSize {
		out.Items = out.Items[:pageSize]
		last := out.Items[pageSize-1]
		scope.LastPosition, scope.LastID = strconv.FormatInt(last.position, 10), last.ID.String()
		cursor, encodeErr := codec.Encode(scope)
		if encodeErr != nil {
			return out, encodeErr
		}
		out.NextCursor = &cursor
	}
	return out, nil
}

func (s Service) MediaLinks(ctx context.Context, token, csrf string, request MediaRequest) (MediaLinks, error) {
	out := MediaLinks{Items: []MediaLink{}}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var err error
	request, err = normalizeMediaRequest(request)
	if err != nil {
		return out, err
	}
	if _, err := s.Authorize(ctx, token, csrf, "view", true); err != nil {
		return out, err
	}
	type resolved struct {
		itemID                      uuid.UUID
		variant                     string
		object                      objectstore.Object
		width, height               int
		itemOrdinal, variantOrdinal int64
	}
	resolvedItems := make([]resolved, 0, len(request.ItemIDs)*len(request.Variants))
	rows, err := s.DB.Query(ctx, `SELECT item_ordinal,variant_ordinal,outcome,item_id,variant,object_key,object_version,sha256,bytes,media_type,width,height
		FROM photo.resolve_portal_media($1,$2,$3,$4)`, auth.MAC(s.Pepper, "portal-session", token), s.Bucket, request.ItemIDs, request.Variants)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r resolved
		var outcome string
		var key, version, sha, media *string
		var bytes *int64
		var width, height *int
		if err = rows.Scan(&r.itemOrdinal, &r.variantOrdinal, &outcome, &r.itemID, &r.variant, &key, &version, &sha, &bytes, &media, &width, &height); err != nil {
			return out, err
		}
		if outcome == "not_found" {
			return MediaLinks{}, domain.NewError("NOT_FOUND", 404)
		}
		if outcome != "ok" || key == nil || version == nil || sha == nil || bytes == nil || media == nil || width == nil || height == nil {
			return MediaLinks{}, domain.NewError("MEDIA_NOT_READY", 409)
		}
		r.object = objectstore.Object{Key: *key, VersionID: *version, SHA256: *sha, Bytes: *bytes, ContentType: *media}
		r.width, r.height = *width, *height
		resolvedItems = append(resolvedItems, r)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(resolvedItems) != len(request.ItemIDs)*len(request.Variants) {
		return MediaLinks{}, domain.NewError("INVALID_GUEST_SESSION", 401)
	}
	for index, r := range resolvedItems {
		expectedItem, expectedVariant := index/len(request.Variants), index%len(request.Variants)
		if r.itemOrdinal != int64(expectedItem+1) || r.variantOrdinal != int64(expectedVariant+1) || r.itemID != request.ItemIDs[expectedItem] || r.variant != request.Variants[expectedVariant] {
			return MediaLinks{}, errors.New("portal media ordering invariant failed")
		}
	}
	for _, r := range resolvedItems {
		signed, signErr := s.Objects.SignRead(ctx, r.object, time.Minute)
		if signErr != nil {
			return MediaLinks{}, signErr
		}
		out.Items = append(out.Items, MediaLink{ItemID: r.itemID, Variant: r.variant, URL: signed.URL, ExpiresAt: signed.ExpiresAt, Width: r.width, Height: r.height, MediaType: r.object.ContentType})
	}
	return out, nil
}

func (s Service) CreateSelection(ctx context.Context, token, csrf, key string, raw SelectionRequest) (SnapshotRef, error) {
	var out SnapshotRef
	if s.DB == nil || len(s.Pepper) < 32 {
		return out, errors.New("portal dependencies unavailable")
	}
	request, revision, sectionID, err := normalizeSelection(raw)
	if err != nil {
		return out, err
	}
	if !idempotency.ValidKey(key) {
		return out, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	requestHash, err := canonical.Hash(request)
	if err != nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var outcome string
	var id *uuid.UUID
	var digest *string
	var count *int64
	var expires *time.Time
	err = s.DB.QueryRow(ctx, `SELECT outcome,snapshot_id,digest,item_count,expires_at FROM photo.create_portal_selection($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		auth.MAC(s.Pepper, "portal-session", token), auth.MAC(s.Pepper, "portal-csrf", token, csrf), key, requestHash, uuid.New(), revision, request.Scope, request.ItemIDs, sectionID, s.now().Add(24*time.Hour)).Scan(&outcome, &id, &digest, &count, &expires)
	if err != nil {
		return out, err
	}
	switch outcome {
	case "created", "replayed":
		if id == nil || digest == nil || count == nil || expires == nil {
			return out, errors.New("incomplete portal snapshot row")
		}
		return SnapshotRef{ID: *id, Digest: *digest, ItemCount: *count, ExpiresAt: *expires}, nil
	case "conflict":
		return out, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case "stale":
		return out, domain.NewError("SNAPSHOT_STALE", 409)
	case "empty":
		return out, domain.NewError("EMPTY_SELECTION", 422)
	case "unauthorized":
		return out, domain.NewError("INVALID_GUEST_SESSION", 401)
	default:
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
}

func (s Service) StartDownload(ctx context.Context, token, csrf, key string, request DownloadRequest) (DownloadAccepted, error) {
	var out DownloadAccepted
	if s.DB == nil || len(s.Pepper) < 32 {
		return out, errors.New("portal dependencies unavailable")
	}
	if request.SelectionSnapshotID == uuid.Nil || request.PresetID == uuid.Nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return out, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	requestHash, err := canonical.Hash(request)
	if err != nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var outcome string
	var id *uuid.UUID
	var status *string
	err = s.DB.QueryRow(ctx, `SELECT outcome,job_id,status FROM photo.start_portal_download($1,$2,$3,$4,$5,$6,$7,$8)`,
		auth.MAC(s.Pepper, "portal-session", token), auth.MAC(s.Pepper, "portal-csrf", token, csrf), key, requestHash,
		uuid.New(), uuid.New(), request.SelectionSnapshotID, request.PresetID).Scan(&outcome, &id, &status)
	if err != nil {
		return out, err
	}
	switch outcome {
	case "created", "replayed":
		if id == nil || status == nil {
			return out, errors.New("incomplete portal download row")
		}
		return DownloadAccepted{DownloadJobID: *id, Status: *status}, nil
	case "conflict":
		return out, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case "snapshot_invalid":
		return out, domain.NewError("SNAPSHOT_STALE", 409)
	case "revoked", "unauthorized":
		return out, domain.NewError("INVALID_GUEST_SESSION", 401)
	case "preset_invalid":
		return out, domain.NewError("PRESET_NOT_ALLOWED", 403)
	default:
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
}

func scanDownload(row pgx.Row, withOutcome bool) (DownloadStatus, string, error) {
	var out DownloadStatus
	var volumes, failure []byte
	var outcome string
	var id *uuid.UUID
	var status *string
	var filesDone, filesTotal, bytesWritten *int64
	var expiresAt *time.Time
	var err error
	if withOutcome {
		err = row.Scan(&outcome, &id, &status, &filesDone, &filesTotal, &bytesWritten, &volumes, &expiresAt, &failure)
	} else {
		err = row.Scan(&id, &status, &filesDone, &filesTotal, &bytesWritten, &volumes, &expiresAt, &failure)
	}
	if err != nil {
		return out, outcome, err
	}
	if withOutcome && outcome != "ok" {
		return out, outcome, nil
	}
	if id == nil || status == nil || filesDone == nil || filesTotal == nil || bytesWritten == nil {
		return DownloadStatus{}, outcome, errors.New("incomplete portal download projection")
	}
	out.ID, out.Status, out.FilesDone, out.FilesTotal, out.BytesWritten, out.ExpiresAt = *id, *status, *filesDone, *filesTotal, *bytesWritten, expiresAt
	if err = json.Unmarshal(volumes, &out.Volumes); err != nil || out.ID == uuid.Nil || out.FilesDone < 0 || out.FilesTotal < out.FilesDone || out.BytesWritten < 0 {
		return DownloadStatus{}, outcome, errors.New("invalid portal download projection")
	}
	if len(failure) > 0 && string(failure) != "null" {
		out.Error = append(json.RawMessage(nil), failure...)
	}
	return out, outcome, nil
}

func (s Service) Download(ctx context.Context, token string, id uuid.UUID) (DownloadStatus, error) {
	if s.DB == nil || len(s.Pepper) < 32 || id == uuid.Nil {
		return DownloadStatus{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	out, _, err := scanDownload(s.DB.QueryRow(ctx, `SELECT id,status,files_done,files_total,bytes_written,volumes,expires_at,error FROM photo.get_portal_download($1,$2)`,
		auth.MAC(s.Pepper, "portal-session", token), id), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return DownloadStatus{}, domain.NewError("NOT_FOUND", 404)
	}
	return out, err
}

func (s Service) CancelDownload(ctx context.Context, token, csrf string, id uuid.UUID) (DownloadStatus, error) {
	if s.DB == nil || len(s.Pepper) < 32 || id == uuid.Nil {
		return DownloadStatus{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	out, outcome, err := scanDownload(s.DB.QueryRow(ctx, `SELECT outcome,id,status,files_done,files_total,bytes_written,volumes,expires_at,error FROM photo.cancel_portal_download($1,$2,$3)`,
		auth.MAC(s.Pepper, "portal-session", token), auth.MAC(s.Pepper, "portal-csrf", token, csrf), id), true)
	if err != nil {
		return DownloadStatus{}, err
	}
	switch outcome {
	case "ok":
		return out, nil
	case "unauthorized":
		return DownloadStatus{}, domain.NewError("INVALID_GUEST_SESSION", 401)
	case "not_found":
		return DownloadStatus{}, domain.NewError("NOT_FOUND", 404)
	default:
		return DownloadStatus{}, domain.NewError("VALIDATION_FAILED", 422)
	}
}

func (s Service) CreateDownloadIntent(ctx context.Context, token, csrf, key string, job uuid.UUID, request DownloadIntentRequest) (DownloadIntent, error) {
	if s.DB == nil || len(s.Pepper) < 32 || job == uuid.Nil || request.Volume < 1 || request.Volume > 5000 {
		return DownloadIntent{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return DownloadIntent{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	requestHash, err := canonical.Hash(struct {
		Job    uuid.UUID `json:"download_job_id"`
		Volume int       `json:"volume"`
	}{job, request.Volume})
	if err != nil {
		return DownloadIntent{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	intentID := uuid.New()
	bearer := base64.RawURLEncoding.EncodeToString(auth.MAC(s.Pepper, "portal-download-bearer", intentID.String()))
	var outcome string
	var id *uuid.UUID
	var expires *time.Time
	err = s.DB.QueryRow(ctx, "SELECT outcome,intent_id,expires_at FROM photo.create_portal_download_intent($1,$2,$3,$4,$5,$6,$7,$8,$9)",
		auth.MAC(s.Pepper, "portal-session", token), auth.MAC(s.Pepper, "portal-csrf", token, csrf), key, requestHash, job, request.Volume, intentID, auth.MAC(s.Pepper, "portal-download-intent", bearer), s.now().Add(time.Minute)).Scan(&outcome, &id, &expires)
	if err != nil {
		return DownloadIntent{}, err
	}
	switch outcome {
	case "created", "replayed":
		if id == nil || expires == nil {
			return DownloadIntent{}, errors.New("incomplete download intent")
		}
		bearer = base64.RawURLEncoding.EncodeToString(auth.MAC(s.Pepper, "portal-download-bearer", id.String()))
		return DownloadIntent{Token: bearer, ExpiresAt: *expires}, nil
	case "conflict":
		return DownloadIntent{}, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case "unauthorized":
		return DownloadIntent{}, domain.NewError("INVALID_GUEST_SESSION", 401)
	case "not_ready":
		return DownloadIntent{}, domain.NewError("DOWNLOAD_NOT_READY", 409)
	default:
		return DownloadIntent{}, domain.NewError("VALIDATION_FAILED", 422)
	}
}

func (s Service) CreateItemDownloadIntent(ctx context.Context, token, csrf, key string, item uuid.UUID, request SingleDownloadRequest) (DownloadIntent, error) {
	if s.DB == nil || len(s.Pepper) < 32 || item == uuid.Nil || request.PresetID == uuid.Nil {
		return DownloadIntent{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return DownloadIntent{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	requestHash, err := canonical.Hash(struct {
		Item     uuid.UUID `json:"item_id"`
		PresetID uuid.UUID `json:"preset_id"`
	}{item, request.PresetID})
	if err != nil {
		return DownloadIntent{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	intentID := uuid.New()
	bearer := base64.RawURLEncoding.EncodeToString(auth.MAC(s.Pepper, "portal-download-bearer", intentID.String()))
	var outcome string
	var id *uuid.UUID
	var expires *time.Time
	err = s.DB.QueryRow(ctx, "SELECT outcome,intent_id,expires_at FROM photo.create_portal_item_download_intent($1,$2,$3,$4,$5,$6,$7,$8,$9)",
		auth.MAC(s.Pepper, "portal-session", token), auth.MAC(s.Pepper, "portal-csrf", token, csrf), key, requestHash, item, request.PresetID, intentID, auth.MAC(s.Pepper, "portal-download-intent", bearer), s.now().Add(time.Minute)).Scan(&outcome, &id, &expires)
	if err != nil {
		return DownloadIntent{}, err
	}
	switch outcome {
	case "created", "replayed":
		if id == nil || expires == nil {
			return DownloadIntent{}, errors.New("incomplete item download intent")
		}
		bearer = base64.RawURLEncoding.EncodeToString(auth.MAC(s.Pepper, "portal-download-bearer", id.String()))
		return DownloadIntent{Token: bearer, ExpiresAt: *expires}, nil
	case "conflict":
		return DownloadIntent{}, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case "unauthorized":
		return DownloadIntent{}, domain.NewError("INVALID_GUEST_SESSION", 401)
	case "not_ready":
		return DownloadIntent{}, domain.NewError("DOWNLOAD_NOT_READY", 409)
	case "preset_invalid", "revoked":
		return DownloadIntent{}, domain.NewError("DOWNLOAD_NOT_ALLOWED", 403)
	default:
		return DownloadIntent{}, domain.NewError("VALIDATION_FAILED", 422)
	}
}

func (s Service) RedeemDownloadIntent(ctx context.Context, request DownloadRedeemRequest) (DownloadLink, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || len(s.Pepper) < 32 {
		return DownloadLink{}, errors.New("portal dependencies unavailable")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(request.Token)
	if err != nil || len(decoded) != 32 {
		return DownloadLink{}, domain.NewError("DOWNLOAD_INTENT_INVALID", 404)
	}
	var outcome string
	var key, version, sha, media, filename *string
	var size *int64
	err = s.DB.QueryRow(ctx, "SELECT outcome,object_key,object_version,sha256,bytes,media_type,filename FROM photo.redeem_download_intent($1,$2)", auth.MAC(s.Pepper, "portal-download-intent", request.Token), s.Bucket).Scan(&outcome, &key, &version, &sha, &size, &media, &filename)
	if err != nil {
		return DownloadLink{}, err
	}
	if outcome != "ok" || key == nil || version == nil || sha == nil || size == nil || media == nil || filename == nil {
		return DownloadLink{}, domain.NewError("DOWNLOAD_INTENT_INVALID", 404)
	}
	signed, err := s.Objects.SignDownload(ctx, objectstore.Object{Key: *key, VersionID: *version, SHA256: *sha, Bytes: *size, ContentType: *media}, *filename, time.Minute)
	if err != nil {
		return DownloadLink{}, err
	}
	return DownloadLink{URL: signed.URL, ExpiresAt: signed.ExpiresAt, Filename: *filename, Bytes: *size, SHA256: *sha}, nil
}

func (s Service) BeginUpload(ctx context.Context, token, csrf, key string, raw GuestUploadBegin) (GuestUpload, error) {
	request, media, err := normalizeGuestUpload(raw)
	if err != nil {
		return GuestUpload{}, err
	}
	if !idempotency.ValidKey(key) {
		return GuestUpload{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	if s.Objects == nil {
		return GuestUpload{}, errors.New("portal object storage unavailable")
	}
	identity, err := s.Authorize(ctx, token, csrf, "upload", true)
	if err != nil {
		return GuestUpload{}, err
	}
	requestHash, err := canonical.Hash(request)
	if err != nil {
		return GuestUpload{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	uploadID, reservationID := uuid.New(), uuid.New()
	objectKey, err := objectstore.StagingKey(identity.TenantID, uploadID)
	if err != nil {
		return GuestUpload{}, err
	}
	var outcome string
	out, err := scanGuestUpload(s.DB.QueryRow(ctx, `SELECT outcome,id,project_id,collection_id,state,revision,filename,expected_size,actual_size,expires_at,object_key,multipart_id,media_type
		FROM photo.begin_portal_upload($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		auth.MAC(s.Pepper, "portal-session", token), auth.MAC(s.Pepper, "portal-csrf", token, csrf), key, requestHash,
		uploadID, reservationID, objectKey, request.Filename, request.Bytes, request.Format, media, request.AssumeProfile, request.SHA256, s.now().Add(24*time.Hour)), &outcome)
	if err != nil {
		return GuestUpload{}, err
	}
	switch outcome {
	case "created", "replayed":
	case "quota":
		return GuestUpload{}, domain.NewError("QUOTA_EXCEEDED", 422)
	case "conflict":
		return GuestUpload{}, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case "unauthorized":
		return GuestUpload{}, domain.NewError("INVALID_GUEST_SESSION", 401)
	default:
		return GuestUpload{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if out.State != "initializing" {
		return out, nil
	}
	multipart, err := s.Objects.BeginMultipart(ctx, out.objectKey, out.mediaType)
	if err != nil {
		return GuestUpload{}, err
	}
	var attached bool
	err = s.DB.QueryRow(ctx, "SELECT photo.attach_portal_multipart($1,$2,$3,$4)", auth.MAC(s.Pepper, "portal-session", token), auth.MAC(s.Pepper, "portal-csrf", token, csrf), out.ID, multipart).Scan(&attached)
	if err != nil || !attached {
		abort, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		_ = s.Objects.AbortMultipart(abort, out.objectKey, multipart)
		cancel()
		if err != nil {
			return GuestUpload{}, err
		}
	}
	return s.GetUpload(ctx, token, out.ID)
}

func (s Service) GetUpload(ctx context.Context, token string, id uuid.UUID) (GuestUpload, error) {
	if s.DB == nil || id == uuid.Nil || token == "" {
		return GuestUpload{}, domain.NewError("NOT_FOUND", 404)
	}
	out, err := scanGuestUpload(s.DB.QueryRow(ctx, `SELECT id,project_id,collection_id,state,revision,filename,expected_size,actual_size,expires_at,object_key,multipart_id,media_type
		FROM photo.get_portal_upload($1,$2,false)`, auth.MAC(s.Pepper, "portal-session", token), id), nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return GuestUpload{}, domain.NewError("NOT_FOUND", 404)
	}
	return out, err
}

func (s Service) SignUploadParts(ctx context.Context, token, csrf string, id uuid.UUID, request GuestPartRequest) (GuestPartURLs, error) {
	if len(request.PartNumbers) < 1 || len(request.PartNumbers) > 100 {
		return GuestPartURLs{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if _, err := s.Authorize(ctx, token, csrf, "upload", true); err != nil {
		return GuestPartURLs{}, err
	}
	upload, err := s.GetUpload(ctx, token, id)
	if err != nil {
		return GuestPartURLs{}, err
	}
	if upload.State != "uploading" || upload.multipartID == nil || !upload.ExpiresAt.After(s.now()) {
		return GuestPartURLs{}, domain.NewError("UPLOAD_STATE_INVALID", 409)
	}
	items, err := s.Objects.SignUploadParts(ctx, upload.objectKey, *upload.multipartID, request.PartNumbers, 10*time.Minute)
	if errors.Is(err, objectstore.ErrInvalid) {
		return GuestPartURLs{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return GuestPartURLs{UploadID: id, Items: items}, err
}

func (s Service) AbortUpload(ctx context.Context, token, csrf string, id uuid.UUID) (GuestUpload, error) {
	if s.Objects == nil {
		return GuestUpload{}, errors.New("portal object storage unavailable")
	}
	out, err := scanGuestUpload(s.DB.QueryRow(ctx, `SELECT id,project_id,collection_id,state,revision,filename,expected_size,actual_size,expires_at,object_key,multipart_id,media_type
		FROM photo.abort_portal_upload($1,$2,$3)`, auth.MAC(s.Pepper, "portal-session", token), auth.MAC(s.Pepper, "portal-csrf", token, csrf), id), nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return GuestUpload{}, domain.NewError("UPLOAD_STATE_INVALID", 409)
	}
	if err != nil {
		return GuestUpload{}, err
	}
	if out.multipartID != nil {
		abort, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		err = s.Objects.AbortMultipart(abort, out.objectKey, *out.multipartID)
		cancel()
		if err != nil && !errors.Is(err, objectstore.ErrMissing) {
			return out, err
		}
	}
	return out, nil
}

func (s Service) CompleteUpload(ctx context.Context, token, csrf, key string, id uuid.UUID, request GuestComplete) (GuestAccepted, error) {
	var out GuestAccepted
	if !idempotency.ValidKey(key) {
		return out, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	upload, err := s.GetUpload(ctx, token, id)
	if err != nil {
		return out, err
	}
	if !validGuestCompletion(request, upload) {
		return out, domain.NewError("UPLOAD_STATE_INVALID", 409)
	}
	requestHash, err := canonical.Hash(request)
	if err != nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	parts, err := canonical.Marshal(request.Parts)
	if err != nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var outcome string
	var jobID *uuid.UUID
	var status *string
	err = s.DB.QueryRow(ctx, `SELECT outcome,job_id,status FROM photo.complete_portal_upload($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)`,
		auth.MAC(s.Pepper, "portal-session", token), auth.MAC(s.Pepper, "portal-csrf", token, csrf), key, requestHash, id, request.ExpectedSize, string(parts), uuid.New(), uuid.New(), uuid.New()).Scan(&outcome, &jobID, &status)
	if err != nil {
		return GuestAccepted{}, err
	}
	switch outcome {
	case "accepted", "replayed":
		if jobID == nil || status == nil {
			return GuestAccepted{}, errors.New("incomplete portal completion row")
		}
		out.JobID, out.Status = *jobID, *status
		return out, nil
	case "conflict":
		return GuestAccepted{}, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case "unauthorized":
		return GuestAccepted{}, domain.NewError("INVALID_GUEST_SESSION", 401)
	case "state":
		return GuestAccepted{}, domain.NewError("UPLOAD_STATE_INVALID", 409)
	default:
		return GuestAccepted{}, domain.NewError("VALIDATION_FAILED", 422)
	}
}
