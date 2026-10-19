package asset

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"denova/config"
	"denova/internal/assetstore"
	"denova/internal/book"
	"denova/internal/book/lore"
	imagegen "denova/internal/image/generation"
)

const (
	LoreResultSchema    = "lore_item_image.v1"
	defaultImageSize    = "2048x2048"
	defaultOutputFormat = "png"
	maxPresetChars      = 4000
	maxBriefChars       = 1000
	maxContentChars     = 4000
	maxInstructionChars = 1000
)

type LoreGenerateRequest struct {
	Provenance ProvenanceStorage
	Item       lore.Item
	// Prompt is a complete provider prompt. When set, no preset or natural-
	// language template is added.
	Prompt            string
	Instruction       string
	ImagePresetID     string
	ImagePresetPrompt string
	ProfileID         string
	Size              string
	Quality           string
	OutputFormat      string
}

func (s *Service) GenerateLore(ctx context.Context, cfg *config.Config, bookService *book.Service, request LoreGenerateRequest) (lore.Image, error) {
	if s == nil {
		s = NewService()
	}
	if s.generator == nil {
		s.generator = imagegen.NewService()
	}
	if cfg == nil {
		return lore.Image{}, fmt.Errorf("运行配置不可用")
	}
	if bookService == nil || strings.TrimSpace(bookService.Workspace()) == "" {
		return lore.Image{}, fmt.Errorf("workspace 不可用")
	}
	item := request.Item
	if strings.TrimSpace(item.ID) == "" {
		return lore.Image{}, fmt.Errorf("资料 ID 不能为空")
	}
	if strings.TrimSpace(item.Name) == "" {
		return lore.Image{}, fmt.Errorf("资料名称不能为空")
	}
	prompt := BuildLorePrompt(request)
	if prompt == "" {
		return lore.Image{}, imagegen.ErrPromptRequired
	}

	generated, err := s.generator.Generate(ctx, cfg, imagegen.GenerateRequest{
		ProfileID:    strings.TrimSpace(request.ProfileID),
		Prompt:       prompt,
		N:            1,
		Size:         firstNonEmpty(request.Size, defaultImageSize),
		Quality:      strings.TrimSpace(request.Quality),
		OutputFormat: firstNonEmpty(request.OutputFormat, defaultOutputFormat),
	})
	if err != nil {
		return lore.Image{}, err
	}
	if len(generated.Images) == 0 {
		return lore.Image{}, fmt.Errorf("图像模型未返回图像")
	}
	image := generated.Images[0]
	if len(image.Data) == 0 {
		return lore.Image{}, fmt.Errorf("图像模型返回了空图像")
	}
	if err := ctx.Err(); err != nil {
		return lore.Image{}, err
	}
	ext := normalizeImageExtension(image.Extension, generated.OutputFormat, request.OutputFormat, defaultOutputFormat)
	if ext == "" {
		return lore.Image{}, fmt.Errorf("无法识别图像格式")
	}

	createdAt := s.now().UTC()
	imagePath := assetstore.NewPath(assetstore.Lore, ext)

	result := lore.Image{
		Schema:        LoreResultSchema,
		ImagePath:     imagePath,
		AltText:       defaultLoreAltText(item),
		ImagePresetID: strings.TrimSpace(request.ImagePresetID),
		ProfileID:     generated.ProfileID,
		Provider:      generated.Provider,
		Model:         generated.Model,
		Size:          generated.Size,
		Quality:       generated.Quality,
		OutputFormat:  firstNonEmpty(generated.OutputFormat, ext),
		CreatedAt:     createdAt.Format(time.RFC3339),
		RevisedPrompt: image.RevisedPrompt,
		MIMEType:      image.MIMEType,
		SizeBytes:     len(image.Data),
	}
	saved, err := request.Provenance.file(imagePath, image.Data, generationMeta{
		Prompt: prompt, RevisedPrompt: result.RevisedPrompt, ImagePresetID: result.ImagePresetID,
		ProfileID: result.ProfileID, Provider: result.Provider, Model: result.Model,
		Size: result.Size, Quality: result.Quality, OutputFormat: result.OutputFormat, CreatedAt: result.CreatedAt,
	})
	if err != nil {
		return lore.Image{}, err
	}
	if err := assetstore.Save(ctx, bookService.Workspace(), saved); err != nil {
		return lore.Image{}, fmt.Errorf("save lore image: %w", err)
	}
	if saved.Generation != nil {
		result.MetaPath = assetstore.MetaPath(imagePath)
	}

	return result, nil
}

func BuildLorePrompt(request LoreGenerateRequest) string {
	if prompt := strings.TrimSpace(request.Prompt); prompt != "" {
		return prompt
	}
	item := request.Item
	preset := trimRunes(request.ImagePresetPrompt, maxPresetChars)
	brief := trimRunes(item.BriefDescription, maxBriefChars)
	content := trimRunes(item.Content, maxContentChars)
	instruction := trimRunes(request.Instruction, maxInstructionChars)
	var sb strings.Builder
	if preset != "" {
		sb.WriteString("# Image Style Requirements\n\n")
		sb.WriteString(preset)
		sb.WriteString("\n\n")
	}
	sb.WriteString("# Current Lore Image Request\n\n")
	sb.WriteString("Generate one visual reference for this lore item that works as a setting-card preview and creative reference. Emphasize the subject, identity, or rule imagery so the item is recognizable in a lore list. Do not generate text, titles, author names, watermarks, logos, UI panels, or QR codes.\n\n")
	writePromptLine(&sb, "Lore type", item.Type)
	writePromptLine(&sb, "Lore name", item.Name)
	if len(item.Tags) > 0 {
		writePromptLine(&sb, "Tags", strings.Join(item.Tags, ", "))
	}
	if len(item.Keywords) > 0 {
		writePromptLine(&sb, "Keywords", strings.Join(item.Keywords, ", "))
	}
	if brief != "" {
		sb.WriteString("\n## Brief\n\n")
		sb.WriteString(brief)
		sb.WriteString("\n")
	}
	if content != "" {
		sb.WriteString("\n## Lore Body Excerpt\n\n")
		sb.WriteString(content)
		sb.WriteString("\n")
	}
	if instruction != "" {
		sb.WriteString("\n## Additional User Requirements\n\n")
		sb.WriteString(instruction)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func writePromptLine(sb *strings.Builder, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	sb.WriteString("- ")
	sb.WriteString(key)
	sb.WriteString(": ")
	sb.WriteString(value)
	sb.WriteString("\n")
}

func defaultLoreAltText(item lore.Item) string {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		return "资料项图片"
	}
	return "资料项图片：" + name
}

func loreTypeLabel(value string) string {
	switch strings.TrimSpace(value) {
	case "character":
		return "角色"
	case "world":
		return "世界观"
	case "location":
		return "地点"
	case "faction":
		return "势力"
	case "rule":
		return "规则"
	case "item":
		return "物品"
	default:
		return "资料"
	}
}

// DiscardUnlinkedLore cleans up only a newly generated result owned by the
// caller. Keep files if a failed commit may already have published references.
func DiscardUnlinkedLore(ctx context.Context, store *lore.Store, image lore.Image) {
	if err := store.DiscardUnlinkedMaterial(context.Background(), image.ImagePath); err != nil {
		slog.WarnContext(ctx, "[lore-material] cleanup uncommitted generation failed", "path", image.ImagePath, "error", err)
	}
}
