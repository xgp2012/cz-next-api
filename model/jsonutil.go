package model

import "encoding/json"

// jsonUnmarshal 是对 encoding/json 的薄封装，集中在一处便于将来替换。
func jsonUnmarshal(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}
