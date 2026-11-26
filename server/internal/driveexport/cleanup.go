package driveexport

import (
	"context"
	"errors"
	"strings"
	"time"

	"officerdashboard/internal/files"
	"officerdashboard/internal/store"
)

// coverSweepEvery is how often every live event folder is listed for stale
// cover files that have no export state (for example from an earlier exporter).
// Between sweeps only events whose cover state is stale are listed.
const coverSweepEvery = 24 * time.Hour

// Cleanup uses one shared Events listing, rather than discovering every event's
// folder again. It touches only folders this export can prove it wrote: the
// folder's event id must have drive_export_state under the current root. A
// foreign root, a dev database or an older restored database therefore loses
// nothing. Inside an owned folder it trashes only the files the export writes
// (event.json, summary.md, cover.*), never subfolders such as Design/.
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
	// Ownership is decided once, before any state is forgotten, so duplicate
	// folders of one event are all handled in the same pass.
	owned := map[string]bool{}
	for _, folder := range folders {
		if id, ok := folderEventID(folder.Name); ok && folder.IsFolder() {
			owned[id] = hasPrefix(states, stateKey(drive, "Events/"+id+"/"))
		}
	}
	sweep := s.lastCoverSweep.IsZero() || time.Since(s.lastCoverSweep) >= coverSweepEvery
	var failures []error
	for _, folder := range folders {
		if !folder.IsFolder() {
			continue
		}
		id, ok := folderEventID(folder.Name)
		if !ok || !owned[id] {
			continue
		}
		e, exists := live[id]
		if !exists {
			// A design upload may have created a new event folder after our snapshot.
			// Recheck existence before retiring any folder absent from that snapshot.
			_, err = s.DB.Read().EventByID(ctx, id)
			if err == nil {
				continue
			}
			if errors.Is(err, store.ErrNotFound) {
				err = s.retireEvent(ctx, drive, states, folder, id)
			}
		} else {
			err = s.cleanupCovers(ctx, drive, states, folder, e, sweep)
		}
		if err != nil {
			failures = append(failures, err)
			if stopPass(err) {
				return errors.Join(failures...)
			}
		}
	}
	if sweep && len(failures) == 0 {
		s.lastCoverSweep = time.Now()
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

// exportedName reports the names the export writes directly in an event folder.
func exportedName(name string) bool {
	return name == "event.json" || name == "summary.md" || strings.HasPrefix(name, "cover.")
}

// retireEvent removes a deleted event's exported files. The folder itself is
// trashed only when nothing else remains in it. Otherwise (for example design
// files under Design/, which design_request_files rows may still reference) it
// is renamed "<name> (deleted)": the files stay reachable, and the name no
// longer ends in "[id]", so it is never matched as an event folder again.
func (s *Service) retireEvent(ctx context.Context, drive *files.Drive, states map[string]store.DriveExportState, folder files.Entry, eventID string) error {
	prefix := stateKey(drive, "Events/"+eventID+"/")
	ids := map[string]bool{}
	for path, state := range states {
		if strings.HasPrefix(path, prefix) && state.DriveFileID != "" {
			ids[state.DriveFileID] = true
		}
	}
	children, err := drive.List(ctx, folder.ID)
	if err != nil {
		return err
	}
	var failures []error
	for _, f := range children {
		if f.IsFolder() || !(ids[f.ID] || exportedName(f.Name)) {
			continue
		}
		if err := drive.DeleteByID(ctx, f.ID); err != nil {
			failures = append(failures, err)
			if stopPass(err) {
				break
			}
		}
	}
	if len(failures) > 0 {
		// State is kept, so the next pass still owns the folder and retries.
		return errors.Join(failures...)
	}
	left, err := drive.List(ctx, folder.ID)
	if err != nil {
		return err
	}
	if len(left) == 0 {
		err = drive.DeleteByID(ctx, folder.ID)
	} else {
		err = drive.RenameFolder(ctx, folder.ID, folder.Name+" (deleted)")
	}
	if err != nil {
		return err
	}
	return s.forgetPrefix(ctx, states, prefix)
}

// cleanupCovers lists a live event's folder only when its cover state is stale
// (replaced, re-extensioned, removed, or an earlier trash failed), or on the
// daily sweep.
func (s *Service) cleanupCovers(ctx context.Context, drive *files.Drive, states map[string]store.DriveExportState, folder files.Entry, e eventSnapshot, sweep bool) error {
	current, err := s.DB.Read().EventByID(ctx, e.Event.ID)
	if errors.Is(err, store.ErrNotFound) {
		return s.retireEvent(ctx, drive, states, folder, e.Event.ID)
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
	prefix := stateKey(drive, "Events/"+e.Event.ID+"/cover.")
	if !sweep && !staleCoverState(states, prefix, e.CoverID) {
		return nil
	}
	children, err := drive.List(ctx, folder.ID)
	if err != nil {
		return err
	}
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

// staleCoverState reports cover state (a cover file or the cover.source marker)
// that points anywhere but the current cover.
func staleCoverState(states map[string]store.DriveExportState, prefix, current string) bool {
	for path, state := range states {
		if strings.HasPrefix(path, prefix) && state.DriveFileID != current {
			return true
		}
	}
	return false
}

func hasPrefix(states map[string]store.DriveExportState, prefix string) bool {
	for path := range states {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func sameCoverVersion(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *Service) forgetPrefix(ctx context.Context, states map[string]store.DriveExportState, prefix string) error {
	if !hasPrefix(states, prefix) {
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
