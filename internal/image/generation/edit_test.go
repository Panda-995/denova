package generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"denova/config"
)

func TestImageEditsDeliverSourcesReferencesAndSelection(t *testing.T) {
	var pngData bytes.Buffer
	source := image.NewNRGBA(image.Rect(0, 0, 8, 12))
	source.SetNRGBA(2, 3, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&pngData, source); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(pngData.Bytes())
	for _, protocol := range []string{config.ImageProtocolOpenAI, config.ImageProtocolGemini, config.ImageProtocolArk, config.ImageProtocolXAI} {
		t.Run(protocol, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if protocol == config.ImageProtocolGemini {
					parts := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
					if len(parts) != 3 || parts[1].(map[string]any)["inlineData"].(map[string]any)["data"] != encoded {
						t.Errorf("missing Gemini source/reference: %v", body)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": encoded}}}}}}})
					return
				}
				key := "images"
				if protocol == config.ImageProtocolArk {
					key = "image"
				}
				if len(body[key].([]any)) != 2 {
					t.Errorf("source or reference omitted: %v", body)
				}
				if protocol == config.ImageProtocolOpenAI {
					if r.URL.Path != "/images/edits" {
						t.Errorf("wrong edit endpoint: %s", r.URL.Path)
					}
					maskURL := body["mask"].(map[string]any)["image_url"].(string)
					maskBytes, err := base64.StdEncoding.DecodeString(maskURL[len("data:image/png;base64,"):])
					if err != nil {
						t.Error(err)
						return
					}
					mask, err := png.Decode(bytes.NewReader(maskBytes))
					if err != nil {
						t.Error(err)
						return
					}
					_, _, _, inside := mask.At(3, 4).RGBA()
					_, _, _, outside := mask.At(0, 0).RGBA()
					if inside != 0 || outside != 65535 || mask.Bounds() != source.Bounds() {
						t.Errorf("incorrect selection mask")
					}
				}
				if protocol == config.ImageProtocolXAI && r.URL.Path != "/images/edits" {
					t.Errorf("wrong xAI endpoint")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": encoded}}})
			}))
			defer server.Close()
			request := GenerateRequest{Prompt: "Change the dialogue.", N: 1, Images: []InputImage{{Data: pngData.Bytes(), MIMEType: "image/png"}, {Data: pngData.Bytes(), MIMEType: "image/png"}}, Selection: &ImageRegion{X: .25, Y: .25, Width: .5, Height: .5}}
			prepared, err := prepareImageEdit(request, protocol)
			if err != nil {
				t.Fatal(err)
			}
			var adapter Adapter
			switch protocol {
			case config.ImageProtocolOpenAI:
				adapter = NewOpenAIAdapter(server.Client())
			case config.ImageProtocolGemini:
				adapter = NewGeminiAdapter(server.Client())
			case config.ImageProtocolArk:
				adapter = NewArkAdapter(server.Client())
			case config.ImageProtocolXAI:
				adapter = NewXAIAdapter(server.Client())
			}
			result, err := adapter.Generate(context.Background(), config.ResolvedImageAPIProfile{BaseURL: server.URL, Model: "test", Protocol: protocol}, prepared)
			if err != nil || len(result.Images) != 1 || !bytes.Equal(result.Images[0].Data, pngData.Bytes()) {
				t.Fatalf("edit failed: %v %+v", err, result)
			}
		})
	}
}
