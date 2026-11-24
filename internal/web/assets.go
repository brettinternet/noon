package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"time"
)

//go:embed assets
var assetFiles embed.FS

// assetHash names this build's assets, so URLs change whenever any asset does
// and browsers may cache them forever.
var assetHash = func() string {
	digest := sha256.New()
	_ = fs.WalkDir(assetFiles, "assets", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := assetFiles.ReadFile(name)
		digest.Write([]byte(name))
		digest.Write(data)
		return err
	})
	return hex.EncodeToString(digest.Sum(nil))[:12]
}()

func serveAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	data, err := assetFiles.ReadFile("assets/" + name)
	if r.PathValue("hash") != assetHash || err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
