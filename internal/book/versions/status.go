package versions

import "context"

// Status compares file identities with the current version. Added files need
// metadata only; tracked contents are streamed and respect Project cancellation.
func (s *Service) Status(ctx context.Context, settings VersionAutoSettings) (VersionStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(ctx, settings)
}

func (s *Service) statusLocked(ctx context.Context, settings VersionAutoSettings) (VersionStatus, error) {
	if err := ctx.Err(); err != nil {
		return VersionStatus{}, err
	}
	current, err := s.headVersion()
	if err != nil {
		return VersionStatus{}, err
	}
	baseline := map[string]versionFileData{}
	if current != nil {
		baseline, err = s.commitFileIndex(current.ID)
		if err != nil {
			return VersionStatus{}, err
		}
	}
	changes, err := s.statusChanges(ctx, baseline)
	if err != nil {
		return VersionStatus{}, err
	}
	lastAutoAt, _, err := s.latestVersionTimes()
	if err != nil {
		return VersionStatus{}, err
	}
	settings = normalizeVersionAutoSettings(settings)
	return VersionStatus{
		HasVersions: current != nil,
		Clean:       len(changes) == 0,
		Changes:     changes,
		Latest:      current,
		Auto: VersionAutoInfo{
			TimedEnabled:         settings.TimedEnabled,
			TimedIntervalMinutes: settings.TimedIntervalMinutes,
			Retention:            settings.Retention,
			LastAutoAt:           lastAutoAt,
		},
	}, nil
}

func (s *Service) History(limit int) ([]VersionEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}
	items, err := s.loadVersionHistory(limit)
	if err != nil {
		return nil, err
	}
	return items, nil
}
