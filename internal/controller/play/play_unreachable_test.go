package play

import "testing"

func TestIsBrowserUnreachable(t *testing.T) {
	cases := []struct {
		u    string
		want bool
	}{
		{"http://127.0.0.1:19000/my-media/media/hls/a/seg.ts", true},
		{"http://localhost:19000/my-media/media/hls/a/seg.ts", true},
		{"http://host.docker.internal:19000/my-media/x", true},
		{"http://minio:9000/my-media/x", true},
		{"https://cdn.example.com/hls/a/seg.ts", false},
	}
	for _, c := range cases {
		if got := isBrowserUnreachable(c.u); got != c.want {
			t.Fatalf("%s: got %v want %v", c.u, got, c.want)
		}
	}
}
