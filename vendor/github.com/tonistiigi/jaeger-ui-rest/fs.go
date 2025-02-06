package jaegerui

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/tonistiigi/jaeger-ui-rest/decompress"
)

//go:embed public
var staticFiles embed.FS

func FS(cfg Config) http.FileSystem {
	files, _ := fs.Sub(staticFiles, "public")
	return http.FS(decompress.NewFS(files, cfg))
}
