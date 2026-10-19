package asset

import (
	"bytes"
	"context"
	"fmt"
	imagepkg "image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"denova/config"
	"denova/internal/assetstore"
	"denova/internal/book"
	imagegen "denova/internal/image/generation"

	_ "golang.org/x/image/webp"
)

const (
	CoverResultSchema  = "book_cover.v1"
	CoverPath          = assetstore.CoverPath
	defaultCoverSize   = "1728x2304"
	defaultCoverFormat = "png"
)

type Generator interface {
	Generate(ctx context.Context, cfg *config.Config, request imagegen.GenerateRequest) (imagegen.Result, error)
}

type Service struct {
	generator Generator
	now       func() time.Time
}

type CoverGenerateRequest struct {
	Provenance  ProvenanceStorage
	Title       string
	Description string
	// Prompt is a complete provider prompt. When set, no cover template or
	// image-preset text is added.
	Prompt            string
	Instruction       string
	ImagePresetID     string
	ImagePresetPrompt string
	ProfileID         string
}

type CoverUploadRequest struct {
	Filename string
	Data     []byte
}

type CoverResult struct {
	Schema         string `json:"schema"`
	CoverPath      string `json:"cover_path"`
	SourcePath     string `json:"source_path"`
	MetaPath       string `json:"meta_path,omitempty"`
	BackupPath     string `json:"backup_path,omitempty"`
	CoverUpdatedAt string `json:"cover_updated_at"`
	ImagePresetID  string `json:"image_preset_id,omitempty"`
	ProfileID      string `json:"profile_id"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	OutputFormat   string `json:"output_format,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`

	RevisedPrompt string `json:"revised_prompt,omitempty"`
	MIMEType      string `json:"mime_type,omitempty"`
	SizeBytes     int    `json:"size_bytes,omitempty"`
}

func NewService() *Service {
	return NewServiceWithGenerator(imagegen.NewService())
}

func NewServiceWithGenerator(generator Generator) *Service {
	return &Service{
		generator: generator,
		now:       time.Now,
	}
}

func (s *Service) GenerateCover(ctx context.Context, cfg *config.Config, bookService *book.Service, request CoverGenerateRequest) (CoverResult, error) {
	if s == nil {
		s = NewService()
	}
	if s.generator == nil {
		s.generator = imagegen.NewService()
	}
	if cfg == nil {
		return CoverResult{}, fmt.Errorf("运行配置不可用")
	}
	if bookService == nil || strings.TrimSpace(bookService.Workspace()) == "" {
		return CoverResult{}, fmt.Errorf("workspace 不可用")
	}
	prompt := BuildCoverPrompt(request)
	if prompt == "" {
		return CoverResult{}, imagegen.ErrPromptRequired
	}

	generated, err := s.generator.Generate(ctx, cfg, imagegen.GenerateRequest{
		ProfileID:    strings.TrimSpace(request.ProfileID),
		Prompt:       prompt,
		N:            1,
		Size:         defaultCoverSize,
		OutputFormat: defaultCoverFormat,
	})
	if err != nil {
		return CoverResult{}, err
	}
	if len(generated.Images) == 0 {
		return CoverResult{}, fmt.Errorf("图像模型未返回图像")
	}
	image := generated.Images[0]
	if len(image.Data) == 0 {
		return CoverResult{}, fmt.Errorf("图像模型返回了空图像")
	}
	ext := normalizeImageExtension(image.Extension, generated.OutputFormat, defaultCoverFormat)
	if ext == "" {
		return CoverResult{}, fmt.Errorf("无法识别图像格式")
	}

	createdAt := s.now().UTC()
	sourcePath := assetstore.NewPath(assetstore.Covers, ext)

	displayData := image.Data
	if ext != defaultCoverFormat {
		decoded, _, err := imagepkg.Decode(bytes.NewReader(image.Data))
		if err != nil {
			return CoverResult{}, fmt.Errorf("decode generated cover for PNG conversion: %w", err)
		}
		var converted bytes.Buffer
		if err := png.Encode(&converted, decoded); err != nil {
			return CoverResult{}, fmt.Errorf("convert generated cover to PNG: %w", err)
		}
		displayData = converted.Bytes()
	}

	result := CoverResult{
		Schema:         CoverResultSchema,
		CoverPath:      CoverPath,
		SourcePath:     sourcePath,
		CoverUpdatedAt: createdAt.Format(time.RFC3339Nano),
		ImagePresetID:  strings.TrimSpace(request.ImagePresetID),
		ProfileID:      generated.ProfileID,
		Provider:       generated.Provider,
		Model:          generated.Model,
		Size:           generated.Size,
		Quality:        generated.Quality,
		OutputFormat:   firstNonEmpty(generated.OutputFormat, ext),
		CreatedAt:      createdAt.Format(time.RFC3339),
		RevisedPrompt:  image.RevisedPrompt,
		MIMEType:       image.MIMEType,
		SizeBytes:      len(image.Data),
	}
	saved, err := request.Provenance.file(sourcePath, image.Data, generationMeta{
		Prompt: prompt, RevisedPrompt: result.RevisedPrompt, ImagePresetID: result.ImagePresetID,
		ProfileID: result.ProfileID, Provider: result.Provider, Model: result.Model,
		Size: result.Size, Quality: result.Quality, OutputFormat: result.OutputFormat, CreatedAt: result.CreatedAt,
	})
	if err != nil {
		return CoverResult{}, err
	}
	if err := assetstore.Save(ctx, bookService.Workspace(), saved); err != nil {
		return CoverResult{}, fmt.Errorf("save book cover source: %w", err)
	}
	if saved.Generation != nil {
		result.MetaPath = assetstore.MetaPath(sourcePath)
	}
	result.BackupPath, err = backupExistingCover(bookService)
	if err != nil {
		return CoverResult{}, err
	}
	if err := bookService.WriteBinaryFile(CoverPath, displayData); err != nil {
		return CoverResult{}, fmt.Errorf("write display cover: %w", err)
	}
	if info, statErr := os.Stat(filepath.Join(bookService.Workspace(), filepath.FromSlash(CoverPath))); statErr == nil {
		result.CoverUpdatedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
	}

	return result, nil
}

func (s *Service) UploadCover(bookService *book.Service, request CoverUploadRequest) (CoverResult, error) {
	if s == nil {
		s = NewService()
	}
	if bookService == nil || strings.TrimSpace(bookService.Workspace()) == "" {
		return CoverResult{}, fmt.Errorf("workspace 不可用")
	}
	if len(request.Data) == 0 {
		return CoverResult{}, fmt.Errorf("上传封面为空")
	}

	decoded, format, err := imagepkg.Decode(bytes.NewReader(request.Data))
	if err != nil {
		return CoverResult{}, fmt.Errorf("无法解析封面图片: %w", err)
	}
	sourceExt := normalizeImageExtension(filepath.Ext(request.Filename), format)
	if sourceExt == "" {
		return CoverResult{}, fmt.Errorf("仅支持 PNG 或 JPEG 封面")
	}

	var pngData bytes.Buffer
	if err := png.Encode(&pngData, decoded); err != nil {
		return CoverResult{}, fmt.Errorf("转换封面为 PNG 失败: %w", err)
	}

	createdAt := s.now().UTC()
	sourcePath := assetstore.NewPath(assetstore.Covers, sourceExt)
	if err := assetstore.Save(context.Background(), bookService.Workspace(), assetstore.File{Path: sourcePath, Data: request.Data}); err != nil {
		return CoverResult{}, fmt.Errorf("save uploaded cover source: %w", err)
	}

	backupPath, err := backupExistingCover(bookService)
	if err != nil {
		return CoverResult{}, err
	}
	if err := bookService.WriteBinaryFile(CoverPath, pngData.Bytes()); err != nil {
		return CoverResult{}, fmt.Errorf("写入展示封面失败: %w", err)
	}
	coverUpdatedAt := createdAt.Format(time.RFC3339Nano)
	if info, statErr := os.Stat(filepath.Join(bookService.Workspace(), filepath.FromSlash(CoverPath))); statErr == nil {
		coverUpdatedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
	}

	result := CoverResult{
		Schema:         CoverResultSchema,
		CoverPath:      CoverPath,
		SourcePath:     sourcePath,
		BackupPath:     backupPath,
		CoverUpdatedAt: coverUpdatedAt,
		ProfileID:      "manual",
		Provider:       "user_upload",
		Model:          "manual",
		OutputFormat:   defaultCoverFormat,
		CreatedAt:      createdAt.Format(time.RFC3339),
		MIMEType:       "image/png",
		SizeBytes:      pngData.Len(),
	}
	return result, nil
}

func BuildCoverPrompt(request CoverGenerateRequest) string {
	if prompt := strings.TrimSpace(request.Prompt); prompt != "" {
		return prompt
	}
	title := trimRunes(request.Title, 200)
	description := trimRunes(request.Description, 2000)
	instruction := trimRunes(request.Instruction, 1000)
	preset := strings.TrimSpace(request.ImagePresetPrompt)
	var sb strings.Builder
	if preset != "" {
		sb.WriteString("# 图像风格要求\n\n")
		sb.WriteString(preset)
		sb.WriteString("\n\n")
	}
	sb.WriteString("# 本次封面请求\n\n")
	sb.WriteString("为一本小说生成竖版书籍封面视觉图，画面可作为书架封面。构图必须清晰、有强主体和可识别的题材氛围；不要生成任何文字、书名、作者名、水印、logo、UI 面板或二维码。\n\n")
	if title != "" {
		sb.WriteString("书名：")
		sb.WriteString(title)
		sb.WriteString("\n")
	}
	if description != "" {
		sb.WriteString("简介：")
		sb.WriteString(description)
		sb.WriteString("\n")
	}
	if instruction != "" {
		sb.WriteString("用户生成要求：")
		sb.WriteString(instruction)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func backupExistingCover(bookService *book.Service) (string, error) {
	absCover, err := book.SafePath(bookService.Workspace(), CoverPath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(absCover)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取旧封面失败: %w", err)
	}
	if len(data) == 0 {
		return "", nil
	}
	backupPath := assetstore.NewPath(assetstore.Covers, defaultCoverFormat)
	if err := assetstore.Save(context.Background(), bookService.Workspace(), assetstore.File{Path: backupPath, Data: data}); err != nil {
		return "", fmt.Errorf("backup previous cover: %w", err)
	}
	return backupPath, nil
}

func normalizeImageExtension(values ...string) string {
	for _, value := range values {
		value = strings.ToLower(strings.Trim(strings.TrimSpace(value), "."))
		switch value {
		case "jpg":
			return "jpeg"
		case "jpeg", "png", "webp":
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func trimRunes(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
