package platform

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	imagegen "denova/internal/image/generation"
	"denova/internal/portablepath"
	"denova/internal/revisionfile"
)

// ImageModel is a credential-free projection of a configured image profile.
// ImageInput describes host protocol support, not provider quality guarantees.
type ImageModel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ImageInput bool   `json:"imageInput"`
}

// ImageRequest requests one raster from an explicit catalog model or a bound
// model slot. Exactly one selection is required; it is fixed for the command.
// commandId is stable across transport retries; it must change for a deliberate
// new generation, including retrying an interrupted or cancelled operation.
// Installed plugin commands are unique within their Project, across Sessions.
// Optional output controls pass unchanged to native generation; omitted or
// empty values retain the selected profile's defaults and provider validation.
type ImageRequest struct {
	Source       *AssetRef             `json:"source,omitempty"`
	References   []AssetRef            `json:"references,omitempty"`
	Mask         *AssetRef             `json:"mask,omitempty"`
	Selection    *imagegen.ImageRegion `json:"selection,omitempty"`
	Inputs       []imagegen.InputImage `json:"-"`
	InputMask    *imagegen.InputImage  `json:"-"`
	CommandID    string                `json:"commandId"`
	ModelSlot    string                `json:"modelSlot,omitempty"`
	ModelID      string                `json:"modelId,omitempty"`
	Prompt       string                `json:"prompt" jsonschema_description:"English image prompt; 1..65536 UTF-8 bytes."`
	Size         string                `json:"size,omitempty"`
	AspectRatio  string                `json:"aspectRatio,omitempty"`
	Resolution   string                `json:"resolution,omitempty" jsonschema_description:"Native provider resolution. Omitted or empty uses the selected image profile default."`
	Quality      string                `json:"quality,omitempty"`
	OutputFormat string                `json:"outputFormat,omitempty" jsonschema_description:"Native output format: png, jpeg/jpg or webp. Omitted or empty uses the selected image profile default."`
}

type ImageBytes struct {
	Data          []byte
	RevisedPrompt string
}

type GeneratedImage struct {
	Asset         AssetRef `json:"asset"`
	MIMEType      string   `json:"mimeType"`
	SizeBytes     int      `json:"sizeBytes"`
	RevisedPrompt string   `json:"revisedPrompt,omitempty"`
}

type ImageResult struct {
	CommandID string           `json:"commandId"`
	ModelID   string           `json:"modelId,omitempty"`
	Status    string           `json:"status" jsonschema:"enum=running,enum=completed,enum=failed,enum=cancelled,enum=interrupted"`
	Images    []GeneratedImage `json:"images"`
	Error     *Error           `json:"error,omitempty"`
}

// Direct provider requests have no Agent Session. This resource-owned record
// fences replay before any paid request; an uncertain provider outcome is
// reported as interrupted and never automatically submitted a second time.
type imageReceipt struct {
	Version   int         `json:"version"`
	InputHash string      `json:"inputHash"`
	Result    ImageResult `json:"result"`
}

type imageOperation struct {
	runtime *Runtime
	ctx     context.Context
	cancel  context.CancelFunc
	receipt imageReceipt
	done    chan struct{}
}

// StopRuntime waits for provider cancellation and durable settlement before
// scope files may be upgraded, backed up or removed.
func (s *ResourceService) StopRuntime(ctx context.Context, runtime *Runtime) error {
	s.mu.Lock()
	pending := []<-chan struct{}{}
	for _, operation := range s.live {
		if operation.runtime == runtime {
			operation.cancel()
			pending = append(pending, operation.done)
		}
	}
	s.mu.Unlock()
	for _, done := range pending {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *ResourceService) serveImages(w http.ResponseWriter, request *http.Request, runtime *Runtime, caller *activation, route string) {
	if route == "/images/models" && request.Method == http.MethodGet {
		models, err := s.host.ImageModels(request.Context(), caller.context.Scope.ProjectID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeResponse(w, http.StatusOK, models)
		return
	}
	if route == "/images/generations" && request.Method == http.MethodPost {
		var input ImageRequest
		if err := readRequest(request, &input); err != nil {
			writeError(w, err)
			return
		}
		result, err := s.startImage(request.Context(), runtime, caller, input)
		if err != nil {
			writeError(w, err)
			return
		}
		status := http.StatusOK
		if result.Status == "running" {
			status = http.StatusAccepted
		}
		writeResponse(w, status, result)
		return
	}
	parts := strings.Split(strings.TrimPrefix(route, "/images/generations/"), "/")
	if !strings.HasPrefix(route, "/images/generations/") || len(parts) < 1 || !validImageCommand(parts[0]) {
		writeError(w, failure("NOT_FOUND", "Unknown image route"))
		return
	}
	cancel := request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "cancel"
	if !cancel && (request.Method != http.MethodGet || len(parts) != 1) {
		writeError(w, failure("NOT_FOUND", "Unknown image route"))
		return
	}
	directory, err := imageResourceDirectory(runtime, caller)
	if err != nil {
		writeError(w, err)
		return
	}
	key := imageReceiptPath(directory, parts[0])
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel {
		if operation := s.live[key]; operation != nil {
			operation.cancel()
			slog.Info("platform_image_cancel_requested", "package", caller.release.Manifest.ID, "command", parts[0])
		}
	}
	receipt, err := s.imageReceipt(key)
	if err != nil {
		writeError(w, err)
		return
	}
	writeResponse(w, http.StatusOK, receipt.Result)
}

func validImageCommand(command string) bool {
	return len(command) > 0 && len(command) <= 128 && !strings.ContainsAny(command, "/\\") && strings.TrimSpace(command) == command
}

// Installed Project plugins share image commands and assets across pages and
// conversations. Execution still belongs to the initiating runtime/consumer.
// Games, Story tools and previews retain their isolated resource scopes.
func imageResourceDirectory(runtime *Runtime, caller *activation) (string, error) {
	scope := caller.context.Scope
	if caller.release.Ref.Package.Kind != Plugin || caller.context.Environment != "installed" || (scope.Kind != "project" && scope.Kind != "session") {
		return caller.dataDir + "-resources", nil
	}
	_, layout, err := runtime.manager.registry.Resolve(scope.ProjectID, true)
	if err != nil {
		return "", err
	}
	relative := "extensions/" + caller.release.Manifest.ID + "/images"
	if err := portablepath.CheckPath(layout.StoreRoot, relative); err != nil {
		return "", err
	}
	return filepath.Join(layout.StoreRoot, filepath.FromSlash(relative)), nil
}

func imageReceiptPath(directory, command string) string {
	return filepath.Join(directory, "requests", stableID(command)+".json")
}

// imageReceipt is called with the service mutex held. A running record without
// a live worker belongs to an interrupted process; reading it settles it once.
func (s *ResourceService) imageReceipt(key string) (imageReceipt, error) {
	if current := s.live[key]; current != nil {
		return current.receipt, nil
	}
	var receipt imageReceipt
	if err := readJSON(key, &receipt); err != nil {
		return receipt, err
	}
	if receipt.Version != 1 {
		return receipt, failure("UNSUPPORTED", "Unknown image request record version")
	}
	switch receipt.Result.Status {
	case "running":
		receipt.Result.Status = "interrupted"
		_, receipt.Result.Error = ErrorResponse(failure("RUNTIME_UNAVAILABLE", "Image generation was interrupted; a new command is required to retry"))
		if err := writeJSON(key, receipt); err != nil {
			return receipt, err
		}
	case "completed", "failed", "cancelled", "interrupted":
	default:
		return receipt, failure("INVALID_ARGUMENT", "Unknown image request state")
	}
	return receipt, nil
}

func (s *ResourceService) startImage(requestContext context.Context, runtime *Runtime, caller *activation, input ImageRequest) (ImageResult, error) {
	if !validImageCommand(input.CommandID) || strings.TrimSpace(input.Prompt) == "" || len(input.Prompt) > 64<<10 {
		return ImageResult{}, failure("INVALID_ARGUMENT", "Image command is required and prompt must contain 1 to 65536 bytes")
	}
	if (input.ModelID == "") == (input.ModelSlot == "") {
		return ImageResult{}, failure("INVALID_ARGUMENT", "Select exactly one modelId or modelSlot")
	}
	profileID := input.ModelID
	if input.ModelSlot != "" {
		if !slices.ContainsFunc(caller.release.Manifest.ModelSlots, func(slot ModelSlot) bool { return slot.ID == input.ModelSlot && slot.Kind == "image" }) {
			return ImageResult{}, failure("PERMISSION_DENIED", "A declared image model slot is required")
		}
		modelKey := caller.release.Manifest.ID + "/" + input.ModelSlot
		if caller.release.Ref.Package.Kind == Game {
			modelKey = "local:" + input.ModelSlot
		}
		profileID = runtime.models[modelKey]
	} else {
		models, err := s.host.ImageModels(requestContext, caller.context.Scope.ProjectID)
		if err != nil {
			return ImageResult{}, err
		}
		if !slices.ContainsFunc(models, func(model ImageModel) bool { return model.ID == profileID }) {
			return ImageResult{}, failure("NOT_CONFIGURED", "Selected image model is unavailable; choose another model")
		}
	}
	if profileID == "" {
		return ImageResult{}, failure("NOT_CONFIGURED", "Select an image model for %s", input.ModelSlot)
	}
	if len(input.References) > 12 || ((input.Mask != nil || input.Selection != nil) && input.Source == nil) {
		return ImageResult{}, failure("INVALID_ARGUMENT", "Use at most 12 references; region edits require a source image")
	}
	if region := input.Selection; region != nil && !imagegen.ValidRegion(*region) {
		return ImageResult{}, failure("INVALID_ARGUMENT", "Selection must be inside the source image")
	}
	assets := append([]AssetRef{}, input.References...)
	if input.Source != nil {
		assets = append([]AssetRef{*input.Source}, assets...)
	}
	total := 0
	for _, asset := range assets {
		data, err := s.readImageAsset(requestContext, runtime, caller, asset)
		if err != nil {
			return ImageResult{}, err
		}
		mime, err := rasterType(data)
		if err != nil {
			return ImageResult{}, err
		}
		total += len(data)
		if total > MaxAssetBytes {
			return ImageResult{}, failure("LIMIT_EXCEEDED", "Combined image inputs exceed 32 MiB")
		}
		input.Inputs = append(input.Inputs, imagegen.InputImage{Data: data, MIMEType: mime})
	}
	if input.Mask != nil {
		data, err := s.readImageAsset(requestContext, runtime, caller, *input.Mask)
		if err != nil {
			return ImageResult{}, err
		}
		mime, err := rasterType(data)
		if err != nil {
			return ImageResult{}, err
		}
		if total+len(data) > MaxAssetBytes {
			return ImageResult{}, failure("LIMIT_EXCEEDED", "Combined image inputs exceed 32 MiB")
		}
		input.InputMask = &imagegen.InputImage{Data: data, MIMEType: mime}
	}
	hashInput := []any{input, profileID}
	if len(input.Inputs) > 0 {
		hashInput = append(hashInput, input.Inputs, input.InputMask)
	}
	encoded, err := json.Marshal(hashInput)
	if err != nil {
		return ImageResult{}, err
	}
	hash := stableID(string(encoded))
	directory, err := imageResourceDirectory(runtime, caller)
	if err != nil {
		return ImageResult{}, err
	}
	key := imageReceiptPath(directory, input.CommandID)
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, err := s.imageReceipt(key)
	if err == nil {
		if previous.InputHash != hash {
			return ImageResult{}, failure("IDEMPOTENCY_CONFLICT", "Image command already has different input")
		}
		return previous.Result, nil
	}
	if !os.IsNotExist(err) {
		return ImageResult{}, err
	}
	if err := runtime.ctx.Err(); err != nil {
		return ImageResult{}, err
	}
	ctx, cancel := context.WithCancel(runtime.executionContext(requestContext))
	operation := &imageOperation{runtime: runtime, ctx: ctx, cancel: cancel, done: make(chan struct{}), receipt: imageReceipt{
		Version: 1, InputHash: hash, Result: ImageResult{CommandID: input.CommandID, ModelID: profileID, Status: "running", Images: []GeneratedImage{}},
	}}
	if err := writeJSON(key, operation.receipt); err != nil {
		cancel()
		return ImageResult{}, err
	}
	s.live[key] = operation
	launch("platform_image_generation", func() {
		s.finishImage(key, operation, nil, failure("RUNTIME_FAILED", "Image generation worker panicked"))
	}, func() {
		image, err := s.host.GenerateImage(ctx, caller.context.Scope.ProjectID, profileID, input)
		if err == nil {
			err = ctx.Err()
		}
		var saved *GeneratedImage
		if err == nil {
			saved, err = saveGeneratedImage(ctx, directory, input.CommandID, image)
		}
		s.finishImage(key, operation, saved, err)
	})
	slog.Info("platform_image_started", "package", caller.release.Manifest.ID, "command", input.CommandID)
	return operation.receipt.Result, nil
}

func saveGeneratedImage(ctx context.Context, directory, command string, image ImageBytes) (*GeneratedImage, error) {
	mimeType, err := rasterType(image.Data)
	if err != nil {
		return nil, err
	}
	ext := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp", "image/gif": "gif"}[mimeType]
	name := stableID(command) + "." + ext
	file := filepath.Join(directory, "assets", name)
	if _, err := revisionfile.ReplaceIfRevision(ctx, file, revisionfile.MissingRevision, image.Data, revisionfile.Options{FileMode: 0600, DirectoryMode: 0700}); err != nil {
		return nil, err
	}
	return &GeneratedImage{Asset: AssetRef{Kind: "generated", Path: name}, MIMEType: mimeType, SizeBytes: len(image.Data), RevisedPrompt: image.RevisedPrompt}, nil
}

func (s *ResourceService) finishImage(key string, operation *imageOperation, image *GeneratedImage, runErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer operation.cancel()
	defer delete(s.live, key)
	if s.live[key] != operation {
		return
	}
	defer close(operation.done)
	result := &operation.receipt.Result
	result.Status = "completed"
	if image != nil {
		result.Images = append(result.Images, *image)
	}
	if runErr != nil {
		result.Status = "failed"
		if errors.Is(runErr, context.Canceled) || errors.Is(operation.ctx.Err(), context.Canceled) {
			result.Status = "cancelled"
		}
		// Provider diagnostics may contain endpoint details. Keep them in host
		// logs and return only the public localizable failure envelope.
		slog.Warn("platform_image_generation_failed", "command", result.CommandID, "error", runErr)
		publicError := failure("RUNTIME_FAILED", "Image generation did not complete")
		var known *Error
		if errors.As(runErr, &known) && known.Code == "UNSUPPORTED" {
			publicError = failure("UNSUPPORTED", "Selected image model does not support these image inputs")
		}
		_, result.Error = ErrorResponse(publicError)
	}
	if err := writeJSON(key, operation.receipt); err != nil {
		slog.Error("platform_image_receipt_commit_failed", "command", result.CommandID, "error", err)
	}
	slog.Info("platform_image_settled", "command", result.CommandID, "status", result.Status)
}
