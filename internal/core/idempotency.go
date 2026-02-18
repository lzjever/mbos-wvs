package core

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// ComputeRequestHash computes SHA-256(sorted_json(body) + method + path).
func ComputeRequestHash(body json.RawMessage, method, path string) string {
	sorted := sortedJSON(body)
	h := sha256.New()
	h.Write(sorted)
	h.Write([]byte(method))
	h.Write([]byte(path))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// sortedJSON recursively sorts JSON object keys.
func sortedJSON(data json.RawMessage) []byte {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		// Not an object (array, string, number, etc.) — return as-is compact.
		var v interface{}
		if err2 := json.Unmarshal(data, &v); err2 != nil {
			return data
		}
		b, _ := json.Marshal(v)
		return b
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := []byte("{")
	for i, k := range keys {
		if i > 0 {
			result = append(result, ',')
		}
		kb, _ := json.Marshal(k)
		result = append(result, kb...)
		result = append(result, ':')
		result = append(result, sortedJSON(obj[k])...)
	}
	result = append(result, '}')
	return result
}
