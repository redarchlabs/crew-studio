package watch

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
)

// WorkspaceFile reads a CSV or JSON file in the Studio workspace, such as one dropped there by another process.
// Paths are confined to the workspace with os.Root, so neither ".." nor symbolic links can leave it.
type WorkspaceFile struct {
	Dir string
}

func (w WorkspaceFile) Fetch(_ context.Context, req Request) (Fetched, error) {
	src := req.Def.Source
	data, err := w.read(src.File)
	if err != nil {
		return Fetched{}, err
	}
	d := req.Def
	var items []Item
	if src.Format == "csv" {
		items, err = itemsFromCSV(data, d.IDField, d.TrackedFields, d.MaxItems)
	} else {
		var doc any
		if doc, err = decodeJSON(data); err == nil {
			items, err = itemsFromJSON(doc, src.ItemsPath, d.IDField, d.TrackedFields, d.MaxItems)
		}
	}
	if err != nil {
		return Fetched{}, err
	}
	return Fetched{Label: "workspace:" + src.File, Items: items}, nil
}

func (w WorkspaceFile) read(name string) ([]byte, error) {
	if w.Dir == "" {
		return nil, sourceErrorf("the Studio workspace folder is not configured")
	}
	root, err := os.OpenRoot(w.Dir)
	if err != nil {
		return nil, sourceErrorf("the Studio workspace folder cannot be opened")
	}
	defer root.Close()
	// Lstat first: only regular files are opened, so a named pipe cannot block the scheduler and links are refused
	info, err := root.Lstat(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, sourceErrorf("there is no file %s in the workspace", name)
	case err != nil:
		return nil, sourceErrorf("the file %s cannot be opened (it must be inside the workspace)", name)
	case !info.Mode().IsRegular():
		return nil, sourceErrorf("%s is not a regular file", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, sourceErrorf("the file %s cannot be opened (it must be inside the workspace)", name)
	}
	defer f.Close()
	return readCapped(f, maxSourceBytes, "the file")
}

// itemsFromCSV reads a CSV with a header row: every row is an item, columns are fields.
func itemsFromCSV(data []byte, idColumn string, tracked []string, maxItems int) ([]Item, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, sourceErrorf("the CSV file is empty")
	}
	if err != nil {
		return nil, sourceErrorf("the CSV file could not be read: %v", err)
	}
	cols := map[string]int{}
	for i, name := range header {
		cols[strings.TrimSpace(name)] = i
	}
	var missing []string
	for _, name := range append([]string{idColumn}, tracked...) {
		if _, ok := cols[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, sourceErrorf("the CSV file has no column %s", strings.Join(quoteAll(missing), ", "))
	}
	rows, err := r.ReadAll()
	if err != nil {
		return nil, sourceErrorf("the CSV file could not be read: %v", err)
	}
	if len(rows) > maxItems {
		return nil, &LimitError{Count: len(rows), Max: maxItems}
	}
	items := make([]Item, 0, len(rows))
	for i, row := range rows {
		id, err := checkItemID(strings.TrimSpace(cell(row, cols[idColumn])))
		if err != nil {
			return nil, sourceErrorf("row %d: %v in the column %q", i+2, err, idColumn)
		}
		fields := make(map[string]any, len(tracked))
		for _, f := range tracked {
			fields[f] = normalizeValue(cell(row, cols[f]))
		}
		items = append(items, Item{ID: id, Fields: fields})
	}
	return items, checkUniqueIDs(items)
}

func cell(row []string, i int) string {
	if i < len(row) {
		return strings.TrimSpace(row[i])
	}
	return ""
}

func quoteAll(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "\"" + n + "\""
	}
	return out
}
