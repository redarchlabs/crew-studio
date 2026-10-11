package km2

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"crewstudio/internal/model"
)

// FolderError says why a save cannot go to the folder an agent named. Message is safe to show to the agent.
type FolderError struct {
	Status  int
	Code    string
	Message string
}

func (e *FolderError) Error() string { return e.Message }

// FolderLister lists the folders a key can see.
type FolderLister interface {
	Folders(ctx context.Context) ([]Folder, error)
}

// ResolveFolder picks the folder a save goes to. wanted is "" (the connection's default folder), a folder id, or a
// folder name or path (such as Projects/Atlas). Only the connection's folders (model.KM2Folders) may be used; a
// name is looked up in KM2's folder list and matched among them.
func ResolveFolder(ctx context.Context, conn model.Connection, lister FolderLister, wanted string) (Folder, error) {
	allowed := model.KM2Folders(conn.Config)
	if len(allowed) == 0 {
		return Folder{}, &FolderError{Status: http.StatusConflict, Code: "km2_no_folder",
			Message: "Saving is off for the KM2 connection " + conn.Name + ": it has no folders set. An administrator can set a default folder."}
	}
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		def := conn.Config[model.ConfigKM2DefaultFolder]
		if def == "" {
			return Folder{}, &FolderError{Status: http.StatusBadRequest, Code: "km2_no_folder",
				Message: "The KM2 connection has no default folder; name one of these folders: " + strings.Join(allowed, ", ") + "."}
		}
		return Folder{ID: def}, nil
	}
	if idPattern.MatchString(wanted) {
		id := strings.ToLower(wanted)
		if !slices.Contains(allowed, id) {
			return Folder{}, notAllowed(wanted, allowed)
		}
		return Folder{ID: id}, nil
	}
	return folderByName(ctx, lister, wanted, allowed)
}

func folderByName(ctx context.Context, lister FolderLister, wanted string, allowed []string) (Folder, error) {
	folders, err := lister.Folders(ctx)
	if err != nil {
		if StatusOf(err) == http.StatusForbidden {
			return Folder{}, &FolderError{Status: http.StatusBadRequest, Code: "km2_folder_names",
				Message: "Folder names cannot be looked up with this KM2 key; give the folder id instead: " + strings.Join(allowed, ", ") + "."}
		}
		return Folder{}, err
	}
	key := folderKey(wanted)
	var matches []Folder
	var names []string
	for _, f := range folders {
		if !slices.Contains(allowed, f.ID) {
			continue
		}
		names = append(names, fmt.Sprintf("%s (%s)", f.Path, f.ID))
		if folderKey(f.Name) == key || folderKey(f.Path) == key {
			matches = append(matches, f)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Folder{}, &FolderError{Status: http.StatusForbidden, Code: "km2_folder_not_allowed",
			Message: fmt.Sprintf("This crew may not save to a folder named %q. It may save to: %s.", oneLine(wanted, 100), listOr(names, allowed))}
	}
	return Folder{}, &FolderError{Status: http.StatusBadRequest, Code: "km2_folder_ambiguous",
		Message: fmt.Sprintf("More than one allowed folder is named %q; give its path or id: %s.", oneLine(wanted, 100), strings.Join(names, ", "))}
}

func notAllowed(wanted string, allowed []string) *FolderError {
	return &FolderError{Status: http.StatusForbidden, Code: "km2_folder_not_allowed",
		Message: fmt.Sprintf("This crew may not save to folder %s. It may save to: %s.", oneLine(wanted, 60), strings.Join(allowed, ", "))}
}

func listOr(names, ids []string) string {
	if len(names) > 0 {
		return strings.Join(names, ", ")
	}
	return strings.Join(ids, ", ")
}

// folderKey compares names and paths loosely: any case, and "/" or ">" may stand for KM2's "." between levels.
func folderKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, sep := range []string{" / ", "/", " > ", ">"} {
		s = strings.ReplaceAll(s, sep, ".")
	}
	return s
}
