package objectstore

import "os"

// ConfigFromEnv only loads values. New validates origins and credentials before
// creating a client; no credentials are included in configuration error text.
func ConfigFromEnv(development bool) Config {
	return Config{Endpoint: os.Getenv("S3_ENDPOINT"), PublicEndpoint: os.Getenv("S3_PUBLIC_ENDPOINT"), Bucket: os.Getenv("S3_BUCKET"), AccessKey: os.Getenv("S3_ACCESS_KEY"), SecretKey: os.Getenv("S3_SECRET_KEY"), Region: os.Getenv("S3_REGION"), Development: development,CORSOrigins:[]string{os.Getenv("APP_ORIGIN"),os.Getenv("PORTAL_ORIGIN")}}
}
