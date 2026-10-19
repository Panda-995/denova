package lore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func materialImage(t *testing.T, format string) []byte {
	t.Helper()
	var data bytes.Buffer
	picture := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	var err error
	switch format {
	case "png":
		return materialPNG(t)
	case "jpeg":
		err = jpeg.Encode(&data, picture, nil)
	case "gif":
		second := image.NewPaletted(picture.Rect, picture.Palette)
		second.SetColorIndex(0, 0, 1)
		err = gif.EncodeAll(&data, &gif.GIF{Image: []*image.Paletted{picture, second}, Delay: []int{10, 10}})
	case "webp":
		decoded, decodeErr := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4TBEAAAAvAAAAAAfQ//73v/+BiOh/AAA=")
		err = decodeErr
		data.Write(decoded)
	default:
		t.Fatalf("unsupported test image format: %s", format)
	}
	if err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestMaterialImageUploadsPreserveFormatBytesAndCover(t *testing.T) {
	for _, format := range []string{"jpeg", "png", "webp", "gif"} {
		t.Run(format, func(t *testing.T) {
			s := NewStore(t.TempDir())
			if _, err := s.Create(ItemInput{ID: "hero", Name: "Hero"}); err != nil {
				t.Fatal(err)
			}
			data := materialImage(t, format)
			// Source extensions and MIME headers cannot be trusted to identify images.
			item, err := s.UploadMaterial(t.Context(), "hero", "reference.jpg", data)
			if err != nil {
				t.Fatal(err)
			}
			material := item.ResolvedMaterials[0]
			if material.MIMEType != "image/"+format || filepath.Ext(material.Path) != "."+format || material.SizeBytes != len(data) {
				t.Fatalf("incorrect stored image format: %+v", material)
			}
			if filepath.ToSlash(filepath.Dir(material.Path)) != "assets/lore" || material.Source.MetaPath != "" {
				t.Fatalf("upload location or authority changed: %+v", material)
			}
			if _, err := os.Stat(filepath.Join(s.workspace, "assets/lore/meta.json")); !os.IsNotExist(err) {
				t.Fatalf("upload created a second attribute store: %v", err)
			}
			raw, err := os.ReadFile(s.itemsPath())
			if err != nil {
				t.Fatal(err)
			}
			var collection Collection
			if err := json.Unmarshal(raw, &collection); err != nil {
				t.Fatal(err)
			}
			if collection.Version != loreItemsVersion || len(collection.Assets) != 1 || !reflect.DeepEqual(collection.Assets[0], material.Asset) {
				t.Fatalf("items.json lost canonical attributes: %+v", collection)
			}
			stored, err := os.ReadFile(filepath.Join(s.workspace, filepath.FromSlash(material.Path)))
			if err != nil || !bytes.Equal(data, stored) {
				t.Fatalf("original image bytes changed: %v", err)
			}
			if _, err := s.MutateMaterial("hero", MaterialMutation{Op: "cover", AssetID: material.ID}); err != nil {
				t.Fatal(err)
			}
			restored, err := NewStore(s.workspace).ReadAny("hero")
			if err != nil || restored.Image == nil || restored.Image.MIMEType != material.MIMEType || restored.Image.ImagePath != material.Path {
				t.Fatalf("image cover did not survive reopening: %+v, %v", restored.Image, err)
			}
		})
	}
}

func materialPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func materialWAV() []byte {
	data := make([]byte, 60)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], 52)
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 16)
	return data
}
func TestMaterialUploadsPreserveTextRejectInvalidAndRecoverCanceled(t *testing.T) {
	s := NewStore(t.TempDir())
	item, err := s.Create(ItemInput{ID: "hero", Name: "Hero", Type: "character", Content: "Before"})
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{materialPNG(t), materialWAV()} {
		updated, err := s.UploadMaterial(context.Background(), item.ID, "../reference", data)
		if err != nil {
			t.Fatal(err)
		}
		material := updated.ResolvedMaterials[len(updated.ResolvedMaterials)-1]
		disk, err := os.ReadFile(filepath.Join(s.workspace, filepath.FromSlash(material.Path)))
		if err != nil || !bytes.Equal(data, disk) {
			t.Fatalf("file not committed: %v", err)
		}
		if material.OriginalName != "reference" || updated.Image != nil {
			t.Fatal("unsafe name or implicit cover", updated)
		}
	}
	_, err = s.Update(item.ID, ItemInput{Name: item.Name, Type: item.Type, Content: "Stale", BaseRevision: item.UpdatedAt})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale edit was not rejected: %v", err)
	}
	current, _ := s.ReadAny(item.ID)
	_, err = s.Update(item.ID, ItemInput{Name: item.Name, Type: item.Type, Content: "After", BaseRevision: current.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	current, _ = s.ReadAny(item.ID)
	if current.Content != "After" || len(current.ResolvedMaterials) != 2 {
		t.Fatal("text edit dropped media", current)
	}
	before, _ := os.ReadFile(s.itemsPath())
	for _, bad := range [][]byte{[]byte("invalid"), materialWAV()[:45]} {
		if _, err := s.UploadMaterial(context.Background(), item.ID, "bad.png", bad); !errors.Is(err, ErrMaterialInvalid) {
			t.Fatal("invalid upload accepted", err)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.UploadMaterial(canceled, item.ID, "cancel.png", materialPNG(t)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(s.itemsPath())
	if !bytes.Equal(before, after) {
		t.Fatal("failed upload modified metadata")
	}
	dirs, _ := os.ReadDir(filepath.Join(s.workspace, "assets/lore"))
	if len(dirs) != 2 {
		t.Fatalf("uncommitted upload leaked: %v", dirs)
	}
	if _, err := s.MutateMaterial(item.ID, MaterialMutation{Op: "cover", AssetID: current.ResolvedMaterials[1].ID}); err == nil {
		t.Fatal("audio accepted as cover")
	}
}

func TestInvalidNewMaterialsNeverFallBackToLegacy(t *testing.T) {
	for _, material := range []string{`null`, `{}`, `{"entries":[{"asset_id":"missing"}]}`, `{"entries":[],"cover_asset_id":"missing"}`} {
		raw := []byte(`{"version":2,"items":[{"id":"hero","name":"Hero","image":{"image_path":"old.png"},"materials":` + material + `}]}`)
		if _, err := DecodeCollection(raw); err == nil {
			t.Fatalf("invalid materials accepted: %s", raw)
		}
	}
}
