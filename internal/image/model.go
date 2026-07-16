package image

import "time"

type Image struct {
	ID        string
	Extension string
	MediaType string
	Content   []byte
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt *time.Time
}

type Upload struct {
	Content          []byte
	ExpiresInSeconds *int64
}

type CreateInput struct {
	Images []Upload
}

type UpdateInput struct {
	ID               string
	Content          []byte
	ExpiresInSeconds *int64
}

type Result struct {
	ID        string
	Filename  string
	Extension string
	MediaType string
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt *time.Time
}
