package handlers

import (
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	maxPostImageSize = 20 << 20
	maxPostBodySize  = maxPostImageSize + (1 << 20)
)

func savePostImage(r *http.Request) (string, error) {
	file, header, err := r.FormFile("image")
	if err == http.ErrMissingFile {
		return "", nil
	}
	if err != nil {
		return "", errors.New("Unable to read image")
	}
	defer file.Close()
	if header.Size > maxPostImageSize {
		return "", errors.New("Image must be at most 20 MB")
	}
	config, format, err := image.DecodeConfig(file)
	if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 40000000 {
		return "", errors.New("Upload a valid PNG, JPEG, or GIF image of at most 40 megapixels")
	}
	ext := map[string]string{"png": ".png", "jpeg": ".jpg", "gif": ".gif"}[format]
	if ext == "" {
		return "", errors.New("Unsupported image format")
	}
	if _, err = file.Seek(0, 0); err != nil {
		return "", err
	}
	if err = os.MkdirAll("uploads", 0755); err != nil {
		return "", err
	}
	name := filepath.Join("uploads", randomToken()+ext)
	output, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(output, io.LimitReader(file, maxPostImageSize+1))
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(name)
		return "", errors.New("Unable to save image")
	}
	return filepath.ToSlash(name), nil
}
