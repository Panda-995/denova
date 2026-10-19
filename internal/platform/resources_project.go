package platform

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"denova/internal/portablepath"
	"denova/internal/revisionfile"
	workspacechange "denova/internal/workspace/change"
)

// ProjectContentHost uses application-owned file mutation and version services.
// The runtime chooses the Project and validates declared directories first.
type ProjectContentHost interface {
	ReadProjectAsset(context.Context, string, string) ([]byte, error)
	ReadProjectContent(context.Context, string, string) (FileSnapshot, error)
	SaveProjectContent(context.Context, string, ContentDocument) (string, error)
	CreateProjectAsset(context.Context, string, string, []byte) error
}

func projectContentPath(caller *activation, path string) error {
	if portablepath.Validate(path) != nil {
		return failure("INVALID_ARGUMENT", "Content path must be portable and relative")
	}
	root, _, nested := strings.Cut(path, "/")
	if !nested || !slices.Contains(caller.release.Manifest.ProjectContent, root) {
		return failure("PERMISSION_DENIED", "Content path is outside declared Project directories")
	}
	return nil
}

func projectContentError(err error) error {
	if errors.Is(err, revisionfile.ErrRevisionConflict) {
		return failure("DOCUMENT_CONFLICT", "Project asset already exists with different content")
	}
	var change *workspacechange.Error
	if errors.As(err, &change) {
		if change.Code == workspacechange.ErrorCodeNotFound {
			return failure("NOT_FOUND", "Project file does not exist")
		}
		if change.Code == workspacechange.ErrorCodeConflict || change.Code == workspacechange.ErrorCodeRevisionConflict {
			return failure("DOCUMENT_CONFLICT", "Project file changed; reload before saving")
		}
	}
	if os.IsNotExist(err) {
		return failure("NOT_FOUND", "Project file does not exist")
	}
	return err
}

func (s *ResourceService) serveProjectContent(w http.ResponseWriter, request *http.Request, runtime *Runtime, caller *activation, route string) {
	files, available := s.host.(ProjectContentHost)
	if !available {
		writeError(w, failure("NOT_CONFIGURED", "Project file service is unavailable"))
		return
	}
	projectID := caller.context.Scope.ProjectID
	if route == "/project-content/file" {
		var input ContentDocument
		if request.Method == http.MethodGet {
			input.Path = request.URL.Query().Get("path")
		} else if request.Method == http.MethodPut {
			if err := readRequest(request, &input); err != nil {
				writeError(w, err)
				return
			}
		} else {
			writeError(w, failure("NOT_FOUND", "Unknown content method"))
			return
		}
		if err := projectContentPath(caller, input.Path); err != nil {
			writeError(w, err)
			return
		}
		if request.Method == http.MethodGet {
			document, err := files.ReadProjectContent(request.Context(), projectID, input.Path)
			if err != nil {
				writeError(w, projectContentError(err))
				return
			}
			if len(document.Content) > MaxDefinitionBytes/2 {
				writeError(w, failure("LIMIT_EXCEEDED", "Content must be bounded text"))
				return
			}
			writeResponse(w, 200, FileSnapshot{Content: document.Content, Revision: document.Revision})
			return
		}
		if len(input.Content) > MaxDefinitionBytes/2 {
			writeError(w, failure("LIMIT_EXCEEDED", "Content exceeds the text limit"))
			return
		}
		revision, err := files.SaveProjectContent(request.Context(), projectID, input)
		if err != nil {
			writeError(w, projectContentError(err))
			return
		}
		slog.Info("platform_project_content_saved", "project", projectID, "package", caller.release.Manifest.ID, "path", input.Path)
		writeResponse(w, 200, map[string]string{"revision": revision})
		return
	}
	var path string
	var data []byte
	var err error
	switch {
	case route == "/project-content/assets" && request.Method == http.MethodPost:
		path = request.URL.Query().Get("path")
		if err := projectContentPath(caller, path); err != nil {
			writeError(w, err)
			return
		}
		data, err = io.ReadAll(io.LimitReader(request.Body, MaxAssetBytes+1))
	case route == "/project-content/adopt" && request.Method == http.MethodPost:
		var input struct {
			Path  string   `json:"path"`
			Asset AssetRef `json:"asset"`
		}
		if err := readRequest(request, &input); err != nil {
			writeError(w, err)
			return
		}
		path = input.Path
		if err := projectContentPath(caller, path); err != nil {
			writeError(w, err)
			return
		}
		data, err = s.readImageAsset(request.Context(), runtime, caller, input.Asset)
	default:
		writeError(w, failure("NOT_FOUND", "Unknown Project content route"))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	mime, err := rasterType(data)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := files.CreateProjectAsset(request.Context(), projectID, path, data); err != nil {
		writeError(w, projectContentError(err))
		return
	}
	slog.Info("platform_project_asset_adopted", "project", projectID, "package", caller.release.Manifest.ID, "path", path)
	writeResponse(w, 200, GeneratedImage{Asset: AssetRef{Kind: "project", Path: path}, MIMEType: mime, SizeBytes: len(data)})
}

// readImageAsset applies the same scope and permission checks to previews and
// model inputs. No provider may fetch arbitrary user-supplied URLs or host paths.
func (s *ResourceService) readImageAsset(ctx context.Context, runtime *Runtime, caller *activation, asset AssetRef) ([]byte, error) {
	if !slices.Contains(caller.grants, "assets.read") {
		return nil, failure("PERMISSION_DENIED", "assets.read is not granted")
	}
	if portablepath.Validate(asset.Path) != nil {
		return nil, failure("INVALID_ARGUMENT", "Invalid image path")
	}
	switch asset.Kind {
	case "project":
		if strings.HasPrefix(asset.Path, "assets/") {
			return s.host.ReadAsset(ctx, caller.context.Scope.ProjectID, asset.Path)
		}
		if !slices.Contains(caller.grants, "project.content.read") {
			return nil, failure("PERMISSION_DENIED", "project.content.read is not granted")
		}
		if err := projectContentPath(caller, asset.Path); err != nil {
			return nil, err
		}
		files, available := s.host.(ProjectContentHost)
		if !available {
			return nil, failure("NOT_CONFIGURED", "Project file service is unavailable")
		}
		return files.ReadProjectAsset(ctx, caller.context.Scope.ProjectID, asset.Path)
	case "generated":
		directory, err := imageResourceDirectory(runtime, caller)
		if err != nil {
			return nil, err
		}
		return ReadRasterAsset(filepath.Join(directory, "assets"), asset.Path)
	case "shared":
		directory, err := sharedContentDirectory(runtime, caller)
		if err != nil {
			return nil, err
		}
		if strings.Contains(asset.Path, "/") {
			return nil, failure("INVALID_ARGUMENT", "Shared image requires a filename")
		}
		return ReadRasterAsset(filepath.Join(directory, "assets"), asset.Path)
	default:
		return nil, failure("INVALID_ARGUMENT", "Unknown image asset kind")
	}
}
