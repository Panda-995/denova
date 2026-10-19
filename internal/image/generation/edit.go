package generation

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"

	"denova/config"
	_ "golang.org/x/image/webp"
)

func imageDataURL(input InputImage) string {
	return "data:" + input.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(input.Data)
}

func ValidRegion(r ImageRegion) bool {
	return !math.IsNaN(r.X+r.Y+r.Width+r.Height) && r.X >= 0 && r.Y >= 0 && r.Width > 0 && r.Height > 0 && r.X+r.Width <= 1 && r.Y+r.Height <= 1
}

// A region is semantic guidance, not a pixel-preservation guarantee. OpenAI
// additionally receives a transparent mask; other protocols receive coordinates.
func prepareImageEdit(request GenerateRequest, protocol string) (GenerateRequest, error) {
	if request.Selection == nil {
		return request, nil
	}
	r := *request.Selection
	if len(request.Images) == 0 || !ValidRegion(r) {
		return request, fmt.Errorf("selection requires a source image and normalized bounds")
	}
	request.Prompt += fmt.Sprintf("\nEdit only the region of the first image at x=%.4f, y=%.4f, width=%.4f, height=%.4f (normalized from the top-left). Preserve all other content, layout, character identity and text unless explicitly requested.", r.X, r.Y, r.Width, r.Height)
	if protocol != config.ImageProtocolOpenAI || request.Mask != nil {
		return request, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(request.Images[0].Data))
	if err != nil {
		return request, fmt.Errorf("decode source image dimensions: %w", err)
	}
	if cfg.Width*cfg.Height > 40_000_000 {
		return request, fmt.Errorf("source image exceeds selection pixel limit")
	}
	mask := image.NewNRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	x0, y0 := int(r.X*float64(cfg.Width)), int(r.Y*float64(cfg.Height))
	x1, y1 := int(math.Ceil((r.X+r.Width)*float64(cfg.Width))), int(math.Ceil((r.Y+r.Height)*float64(cfg.Height)))
	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			if x < x0 || x >= x1 || y < y0 || y >= y1 {
				mask.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, mask); err != nil {
		return request, err
	}
	request.Mask = &InputImage{Data: output.Bytes(), MIMEType: "image/png"}
	return request, nil
}
