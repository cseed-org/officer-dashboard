package driveexport

import (
	"context"
	"errors"
	"strings"

	"officerdashboard/internal/files"
	"officerdashboard/internal/store"
)

// Cleanup uses one shared Events listing, rather than discovering every event's
// folder again. Its per-folder listings are required to remove stale cover files,
// including files left by an earlier exporter or without local upload state.
func (s *Service) cleanup(ctx context.Context, drive *files.Drive, states map[string]store.DriveExportState, events []eventSnapshot) error {
	root, err := drive.FindChild(ctx, drive.RootID(), files.EventsFolder)
	if err != nil || root == nil {
		return err
	}
	if !root.IsFolder() {
		return errors.New("Events is not a Drive folder")
	}
	folders, err := drive.List(ctx, root.ID)
	if err != nil {
		return err
	}
	live := map[string]eventSnapshot{}
	for _, e := range events {
		live[files.SafeName(e.Event.ID)] = e
	}
	var failures []error
	for _, folder := range folders {
		if !folder.IsFolder() {
			continue
		}
		id, ok := folderEventID(folder.Name)
		if !ok {
			continue
		}
		e, exists := live[id]
		if !exists {
			// A design upload may have created a new event folder after our snapshot.
			// Recheck existence before deleting any folder absent from that snapshot.
			_, err = s.DB.Read().EventByID(ctx, id)
			if err == nil {
				continue
			}
			if errors.Is(err, store.ErrNotFound) {
				err = s.trashEvent(ctx, drive, states, folder.ID, id)
			}
		} else {
			err = s.cleanupCovers(ctx, drive, states, folder.ID, e)
		}
		if err != nil {
			failures = append(failures, err)
			if stopPass(err) {
				return errors.Join(failures...)
			}
		}
	}
	return errors.Join(failures...)
}

func folderEventID(name string) (string, bool) {
	if !strings.HasSuffix(name, "]") {
		return "", false
	}
	at := strings.LastIndex(name, " [")
	if at <= 0 {
		return "", false
	}
	id := name[at+2 : len(name)-1]
	return id, id != ""
}

func (s *Service) trashEvent(ctx context.Context, drive *files.Drive, states map[string]store.DriveExportState, folderID, eventID string) error {
	if err := drive.DeleteByID(ctx, folderID); err != nil {
		return err
	}
	return s.forgetPrefix(ctx, states, stateKey(drive, "Events/"+eventID+"/"))
}

func (s *Service) cleanupCovers(ctx context.Context, drive *files.Drive, states map[string]store.DriveExportState, folderID string, e eventSnapshot) error {
	current, err := s.DB.Read().EventByID(ctx, e.Event.ID)
	if errors.Is(err, store.ErrNotFound) {
		return s.trashEvent(ctx, drive, states, folderID, e.Event.ID)
	}
	if err != nil {
		return err
	}
	if !e.CoverKnown {
		// A transient cover failure must not prevent deleted-event cleanup.
		return nil
	}
	if !sameCoverVersion(current.CoverUpdatedAt, e.Event.CoverUpdatedAt) {
		return nil
	}
	children, err := drive.List(ctx, folderID)
	if err != nil {
		return err
	}
	prefix := stateKey(drive, "Events/"+e.Event.ID+"/cover.")
	var failures []error
	for _, f := range children {
		if f.IsFolder() || !strings.HasPrefix(f.Name, "cover.") || f.ID == e.CoverID {
			continue
		}
		err := drive.DeleteByID(ctx, f.ID)
		if err == nil {
			err = s.forgetFile(ctx, states, prefix, f.ID)
		}
		if err != nil {
			failures = append(failures, err)
			if stopPass(err) {
				return errors.Join(failures...)
			}
		}
	}
	if e.CoverID == "" && len(failures) == 0 {
		if err := s.forgetPrefix(ctx, states, prefix); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func sameCoverVersion(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *Service) forgetPrefix(ctx context.Context, states map[string]store.DriveExportState, prefix string) error {
	found := false
	for path := range states {
		if strings.HasPrefix(path, prefix) {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	if err := s.DB.Write(ctx, func(q *store.Queries) error { return q.DeleteDriveExportPrefix(ctx, prefix) }); err != nil {
		return err
	}
	for path := range states {
		if strings.HasPrefix(path, prefix) {
			delete(states, path)
		}
	}
	return nil
}

func (s *Service) forgetFile(ctx context.Context, states map[string]store.DriveExportState, prefix, id string) error {
	var paths []string
	for path, state := range states {
		if strings.HasPrefix(path, prefix) && state.DriveFileID == id {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	if err := s.DB.Write(ctx, func(q *store.Queries) error {
		for _, path := range paths {
			if err := q.DeleteDriveExportState(ctx, path); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for _, path := range paths {
		delete(states, path)
	}
	return nil
}
