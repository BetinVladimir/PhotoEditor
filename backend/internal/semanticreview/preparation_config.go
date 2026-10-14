package semanticreview

// ReviewPreparationConfig contains trusted installation pins for reconstructing
// the original semantic review. It contains no credentials or approval authority.
type ReviewPreparationConfig struct {
	SchemaVersion       int    `json:"schema_version"`
	ModelSHA            string `json:"review_model_sha"`
	DatasetSHA          string `json:"review_dataset_sha"`
	DocumentBudgetBytes int64  `json:"document_budget_bytes"`
}

func DecodeReviewPreparationConfig(raw []byte) (ReviewPreparationConfig, error) {
	var config ReviewPreparationConfig
	if len(raw) < 2 || len(raw) > 16384 || decodeStrict(raw, &config) != nil || config.SchemaVersion != 1 || !shaPattern.MatchString(config.ModelSHA) || !shaPattern.MatchString(config.DatasetSHA) || config.DocumentBudgetBytes < 2 || config.DocumentBudgetBytes > 64<<20 {
		return ReviewPreparationConfig{}, ErrInvalid
	}
	return config, nil
}
