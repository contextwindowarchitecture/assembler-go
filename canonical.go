package assembler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"unicode/utf16"

	"github.com/ucarion/jcs"
)

// snapshotDigest hashes RFC 8785 JSON after sorting only contract-defined arrays.
func snapshotDigest(snapshot map[string]any) (string, error) {
	normalized := make(map[string]any, len(snapshot))
	for key, value := range snapshot {
		normalized[key] = value
	}
	batches := append([]any(nil), asArray(snapshot["batches"])...)
	sort.Slice(batches, func(i, j int) bool {
		left := asString(asObject(asObject(batches[i])["producer"])["id"])
		right := asString(asObject(asObject(batches[j])["producer"])["id"])
		return utf16Less(left, right)
	})
	for i, rawBatch := range batches {
		batch := asObject(rawBatch)
		copyBatch := make(map[string]any, len(batch))
		for key, value := range batch {
			copyBatch[key] = value
		}
		items, err := sortedCandidates(asArray(batch["items"]))
		if err != nil {
			return "", err
		}
		copyBatch["items"] = items
		excluded, err := sortedRows(asArray(batch["excluded"]), "item_id")
		if err != nil {
			return "", err
		}
		copyBatch["excluded"] = excluded
		batches[i] = copyBatch
	}
	normalized["batches"] = batches
	conflicts := append([]any(nil), asArray(snapshot["conflicts"])...)
	sort.Slice(conflicts, func(i, j int) bool {
		return utf16Less(asString(asObject(conflicts[i])["id"]), asString(asObject(conflicts[j])["id"]))
	})
	for i, rawGroup := range conflicts {
		group := asObject(rawGroup)
		copyGroup := make(map[string]any, len(group))
		for key, value := range group {
			copyGroup[key] = value
		}
		items := append([]any(nil), asArray(group["items"])...)
		sort.Slice(items, func(i, j int) bool {
			return utf16Less(asString(items[i]), asString(items[j]))
		})
		copyGroup["items"] = items
		conflicts[i] = copyGroup
	}
	normalized["conflicts"] = conflicts
	canonical, err := jcs.Format(normalized)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(hash[:]), nil
}

func utf16Less(left, right string) bool {
	a := utf16.Encode([]rune(left))
	b := utf16.Encode([]rune(right))
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

type sortableRow struct {
	value any
	id    string
	key   []byte
	valid bool
}

func sortedCandidates(items []any) ([]any, error) {
	rows := make([]sortableRow, len(items))
	for i, item := range items {
		id, ok := objectValue(item)["id"].(string)
		rows[i] = sortableRow{value: item, id: id, valid: ok && !blank(id)}
		if rows[i].valid {
			encoded, err := jcs.Format(item)
			if err != nil {
				return nil, err
			}
			rows[i].key = []byte(encoded)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].valid != rows[j].valid {
			return rows[i].valid
		}
		if !rows[i].valid {
			return false
		}
		if rows[i].id != rows[j].id {
			return utf16Less(rows[i].id, rows[j].id)
		}
		return bytes.Compare(rows[i].key, rows[j].key) < 0
	})
	result := make([]any, len(rows))
	for i, row := range rows {
		result[i] = row.value
	}
	return result, nil
}

func sortedRows(items []any, field string) ([]any, error) {
	rows := make([]sortableRow, len(items))
	for i, item := range items {
		encoded, err := jcs.Format(item)
		if err != nil {
			return nil, err
		}
		rows[i] = sortableRow{value: item, id: asString(asObject(item)[field]), key: []byte(encoded)}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].id != rows[j].id {
			return utf16Less(rows[i].id, rows[j].id)
		}
		return bytes.Compare(rows[i].key, rows[j].key) < 0
	})
	result := make([]any, len(rows))
	for i, row := range rows {
		result[i] = row.value
	}
	return result, nil
}
