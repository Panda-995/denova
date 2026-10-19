package project

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"denova/internal/book"
)

// Exercises the shared request boundary, including portable path checks and
// completed migrations. Setup is excluded from the measured request cost.
func BenchmarkRegistryResolve(b *testing.B) {
	for _, count := range []int{1, 50} {
		b.Run(fmt.Sprintf("projects_%d", count), func(b *testing.B) {
			root := b.TempDir()
			registry := NewRegistry(root)
			var id string
			for index := range count {
				workspace := filepath.Join(root, "projects", fmt.Sprintf("Book-%03d", index))
				if err := os.MkdirAll(workspace, 0o700); err != nil {
					b.Fatal(err)
				}
				record, err := registry.Add(workspace, TypeBook, "")
				if err != nil {
					b.Fatal(err)
				}
				id = record.ID
				layout, err := registry.Layout(record)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := book.NewMetaStore(root).Write(workspace, layout.StoreRoot, book.BookMeta{Title: record.Name}); err != nil {
					b.Fatal(err)
				}
			}
			if _, _, err := registry.Resolve(id, true); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, _, err := registry.Resolve(id, true); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
