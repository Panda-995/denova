package conversationjournal

import (
	"cmp"
	"context"
	"fmt"
	"slices"
)

// VisitFrom starts at a domain index location rather than scanning
// from a physical sparse anchor. The visitor receives that record and later
// records through the selected transaction, under one stable read lease.
func (journal *Journal) VisitFrom(ctx context.Context, first Location, through Cursor, visit func(Record) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	release, err := journal.lockForRead(ctx)
	if err != nil {
		return err
	}
	defer release()
	if first.Cursor == 0 || first.Cursor > journal.head.Cursor || first.Offset < 0 || first.RecordIndex < 0 {
		return fmt.Errorf("conversation range start is outside the journal")
	}
	if through == 0 || through > journal.head.Cursor {
		through = journal.head.Cursor
	}
	if through < first.Cursor {
		return nil
	}
	_, bytesRead, err := journal.readSelectedLocked(ctx, first,
		func(cursor Cursor) bool { return cursor >= first.Cursor }, through, int(through-first.Cursor+1), func(record Record) error {
			if record.Location.Cursor == first.Cursor && record.Location.RecordIndex < first.RecordIndex {
				return nil
			}
			return visit(record)
		})
	journal.stats.LastRangeBytesRead = bytesRead
	journal.stats.RangeBytesRead += bytesRead
	return err
}

// ReadLocations fetches exact immutable records, in journal order. Locators
// come from the domain index; transaction identity and checksum are verified
// without reading the intervening history.
func (journal *Journal) ReadLocations(ctx context.Context, locations []Location) ([]Record, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	release, err := journal.lockForRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ordered := slices.Clone(locations)
	slices.SortFunc(ordered, func(a, b Location) int {
		if order := cmp.Compare(a.Cursor, b.Cursor); order != 0 {
			return order
		}
		return cmp.Compare(a.RecordIndex, b.RecordIndex)
	})
	var result, transaction []Record
	var cursor Cursor
	var bytesRead int64
	for _, location := range ordered {
		if location.Cursor == 0 || location.Cursor > journal.head.Cursor || location.Offset < 0 || location.RecordIndex < 0 {
			return nil, fmt.Errorf("conversation record location is outside the journal")
		}
		if cursor != location.Cursor {
			var read int64
			transaction, read, err = journal.readSelectedLocked(ctx, location,
				func(selected Cursor) bool { return selected == location.Cursor }, location.Cursor, 1, nil)
			bytesRead += read
			if err != nil {
				return nil, err
			}
			cursor = location.Cursor
		}
		if location.RecordIndex >= len(transaction) || transaction[location.RecordIndex].Location != location {
			return nil, fmt.Errorf("conversation record location does not match the journal")
		}
		result = append(result, transaction[location.RecordIndex])
	}
	journal.stats.LastRangeBytesRead = bytesRead
	journal.stats.RangeBytesRead += bytesRead
	return result, nil
}
