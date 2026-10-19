package project

// The Registry remains canonical. Compare its bytes on every read so external
// edits, even with unchanged size/timestamps, are observed immediately. Only
// decoding and normalization are cached; availability and path checks stay live.
type registryReadCache struct {
	raw  []byte
	data registryData
}

// Mutations receive owned slices and cannot alter the cached durable snapshot
// before their atomic save succeeds.
func cloneRegistryData(data registryData) registryData {
	data.Projects = append([]Record(nil), data.Projects...)
	for index := range data.Projects {
		if archived := data.Projects[index].ArchivedAt; archived != nil {
			value := *archived
			data.Projects[index].ArchivedAt = &value
		}
	}
	data.Order = append([]string(nil), data.Order...)
	return data
}

type preparedProjectStore struct {
	layout  Layout
	receipt string
}
