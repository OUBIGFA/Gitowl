package gitcli

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

func objectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Blob resolves mutable references every time and caches bytes only under the
// actual immutable object ID. Full commit:path resolution is itself immutable.
// Worktrees share the common Git identity; independent repositories remain
// isolated so a cached object cannot manufacture access to a missing object.
func (r *Repository) Blob(ctx context.Context, revision, path string, limit int) ([]byte, error) {
	if strings.ContainsRune(revision+path, 0) {
		return nil, errors.New("invalid Git object path")
	}
	key := "resolve\x00" + r.CommonDir + "\x00" + revision + ":" + path
	read := func() ([]byte, error) {
		// Read size before allocating content. NUL framing preserves filenames
		// containing newlines, and 'missing' is distinct from process failure.
		data, err := r.Run(ctx, strings.NewReader(revision+":"+path+"\x00"), "cat-file", "--batch-check", "-Z")
		if err == nil && strings.HasSuffix(string(data), " missing\x00") {
			return nil, ErrObjectMissing
		}
		return data, err
	}
	var metadata []byte
	var err error
	if objectID(revision) {
		metadata, err = r.engine.load(ctx, key, read)
	} else {
		metadata, err = read()
	}
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(string(metadata), " missing\x00") {
		return nil, ErrObjectMissing
	}
	fields := strings.Fields(strings.TrimSuffix(string(metadata), "\x00"))
	if len(fields) != 3 || !objectID(fields[0]) || fields[1] != "blob" {
		return nil, errors.New("Git returned invalid blob metadata")
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return nil, errors.New("Git returned an invalid object size")
	}
	if size > 64<<20 || limit > 0 && size > int64(limit) {
		return nil, ErrOutputLimit
	}
	oid := fields[0]
	objectKey := "blob\x00" + r.CommonDir + "\x00" + oid
	if data, ok := r.engine.cache.get(objectKey); ok {
		r.engine.hits.Add(1)
		return data, nil
	}
	data, err := r.engine.load(ctx, objectKey, func() ([]byte, error) { return r.Run(ctx, nil, "cat-file", "blob", oid) })
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, errors.New("Git returned an incomplete blob")
	}
	return data, nil
}
