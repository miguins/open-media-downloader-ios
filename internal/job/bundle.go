package job

import "time"

// Bundle describes a complete ZIP containing the media items of one job.
type Bundle struct {
	JobID     string
	FileName  string
	SizeBytes int64
	CreatedAt time.Time
}

// BundleToken grants temporary access to a bundle; only its hash is stored.
type BundleToken struct {
	Hash      []byte
	JobID     string
	OwnerID   string
	CreatedAt time.Time
	ExpiresAt time.Time
}
