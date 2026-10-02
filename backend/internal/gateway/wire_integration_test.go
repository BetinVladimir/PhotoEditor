package gateway

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"photoeditor/backend/internal/objectstore"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func wireCertificates(t *testing.T) (tls.Certificate, tls.Certificate, *x509.CertPool) {
	t.Helper()
	now := time.Now()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "PhotoEditor isolated integration CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	issue := func(serial int64, usage x509.ExtKeyUsage) tls.Certificate {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "isolated peer"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
		der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		leaf, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{Certificate: [][]byte{der, caDER}, PrivateKey: key, Leaf: leaf}
	}
	return issue(2, x509.ExtKeyUsageServerAuth), issue(3, x509.ExtKeyUsageClientAuth), roots
}

func TestIntegrationWireSQLUpload(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	rawTask := os.Getenv("PHOTO_TEST_GATEWAY_TASK")
	if dsn == "" || rawTask == "" {
		t.Skip("isolated database/task/MinIO required")
	}
	task, err := uuid.Parse(rawTask)
	if err != nil {
		t.Fatal(err)
	}
	large := os.Getenv("PHOTO_RUN_GATEWAY_WIRE_LARGE_IO") == "1"
	duration := time.Minute
	if large {
		duration = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	serverCert, clientCert, roots := wireCertificates(t)
	var id TaskIdentity
	err = admin.QueryRow(ctx, "SELECT tenant_id,run_id,id,worker_id,attempt,fencing_token FROM photo.tasks WHERE id=$1", task).Scan(&id.TenantID, &id.RunID, &id.TaskID, &id.WorkerID, &id.Attempt, &id.Fence)
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, "UPDATE photo.worker_registrations SET certificate_sha256=$1 WHERE worker_id=$2", certificateHash(clientCert.Leaf), id.WorkerID)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the real task lease alive during the opt-in multi-gigabyte transfer.
	heartbeatErrors := make(chan error, 1)
	if large {
		heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
		defer stopHeartbeat()
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-heartbeatCtx.Done():
					return
				case <-ticker.C:
					var accepted, cancelled bool
					var deadline time.Time
					err := admin.QueryRow(heartbeatCtx, "SELECT * FROM photo.heartbeat_task($1,$2,$3,$4,'{\"done\":1,\"total\":2}'::jsonb)", id.TaskID, id.Attempt, id.Fence, id.WorkerID).Scan(&accepted, &deadline, &cancelled)
					if err != nil {
						select {
						case heartbeatErrors <- err:
						default:
						}
						return
					}
					if !accepted || cancelled {
						select {
						case heartbeatErrors <- fmt.Errorf("heartbeat accepted=%v cancelled=%v", accepted, cancelled):
						default:
						}
						return
					}
				}
			}
		}()
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_gateway"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	bucket := "wire-gateway-" + uuid.NewString()
	storage, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: bucket, AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	signer, _, _, _ := fixture(t)
	signer.Now = time.Now
	token, err := signer.Issue(id, clientCert.Leaf, time.Now().Add(duration), time.Now().Add(time.Hour), []string{WriteOutputs})
	if err != nil {
		t.Fatal(err)
	}
	maxBytes := int64(100)
	if large {
		maxBytes = 6 << 30
	}
	api := HTTP{Signer: signer, Tasks: Authorizer{DB: pool}, MaxUploadBytes: maxBytes, Uploads: &UploadService{Repository: SQLOutputs{DB: pool, Bucket: bucket}, Objects: storage}}
	server := httptest.NewUnstartedServer(api.Handler())
	server.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	defer server.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{clientCert}, MinVersion: tls.VersionTLS13}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: duration}
	data := []byte(`{"verification":"wire mTLS"}`)
	size := int64(len(data))
	var newBody func() io.Reader = func() io.Reader { return bytes.NewReader(data) }
	if large {
		size = 1<<32 + 1<<20 + 17
		block := make([]byte, 1<<20)
		for i := range block {
			block[i] = byte(i % 251)
		}
		newBody = func() io.Reader { return &gatewayLargeBody{remaining: size, block: block} }
	}
	hash := sha256.New()
	if _, err = io.CopyBuffer(hash, newBody(), make([]byte, 1<<20)); err != nil {
		t.Fatal(err)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	key := uuid.NewString()
	upload := func(fence string) (int, ArtifactRef) {
		request, err := http.NewRequestWithContext(ctx, "PUT", server.URL+"/internal/v1/tasks/"+task.String()+"/outputs/verified", newBody())
		if err != nil {
			t.Fatal(err)
		}
		request.ContentLength = size
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Photo-Worker", id.WorkerID.String())
		request.Header.Set("X-Photo-Attempt", "1")
		request.Header.Set("X-Photo-Fence", fence)
		request.Header.Set("Idempotency-Key", key)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Photo-SHA256", digest)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var ref ArtifactRef
		if response.StatusCode < 300 {
			if err = json.NewDecoder(response.Body).Decode(&ref); err != nil {
				t.Fatal(err)
			}
		}
		return response.StatusCode, ref
	}
	status, ref := upload("1")
	if status != 201 || ref.SHA256 != digest {
		t.Fatal("wire upload", status)
	}
	status, replay := upload("1")
	if status != 200 || replay != ref {
		t.Fatal("wire replay", status)
	}
	status, _ = upload("2")
	if status != 403 {
		t.Fatal("stale fence accepted", status)
	}
	var reserved, used int64
	err = admin.QueryRow(ctx, "SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=$1", id.TenantID).Scan(&reserved, &used)
	if err != nil || reserved != 0 || used != size {
		t.Fatal("quota", reserved, used, err)
	}
	var object objectstore.Object
	err = admin.QueryRow(ctx, "SELECT object_key,object_version,sha256,media_type,bytes FROM photo.artifacts WHERE id=$1", ref.ArtifactID).Scan(&object.Key, &object.VersionID, &object.SHA256, &object.ContentType, &object.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	body, err := storage.Open(ctx, object)
	if err != nil {
		t.Fatal(err)
	}
	readHash := sha256.New()
	actualSize, err := io.CopyBuffer(readHash, body, make([]byte, 1<<20))
	closeErr := body.Close()
	if err != nil || closeErr != nil || actualSize != size || hex.EncodeToString(readHash.Sum(nil)) != digest {
		t.Fatal("version read", err, closeErr)
	}
	select {
	case err := <-heartbeatErrors:
		t.Fatal(err)
	default:
	}
	t.Log("wire SQL/S3 verified bytes=" + strconv.FormatInt(size, 10))
	if err = storage.RemoveConfirmed(ctx, object); err != nil {
		t.Fatal(err)
	}
}
