/*
Copyright AppsCode Inc. and Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package lib

import (
	"sort"
	"testing"
)

func TestCollectImagesExpandsVersionedImage(t *testing.T) {
	// Shape of a rendered kubestash addons.kubestash.com/v1alpha1 Function.
	fn := map[string]any{
		"spec": map[string]any{
			"availableVersions": []any{"16.4", "17.2", "18.2"},
			"image":             "ghcr.io/kubedb/postgres-restic-plugin:v0.29.0_${DB_VERSION}",
			"args": []any{
				"physical-restore",
				"--namespace=${namespace:=default}",
			},
		},
	}

	images := map[string]string{}
	collectImages(fn, images, "Function.addons.kubestash.com")

	got := make([]string, 0, len(images))
	for img := range images {
		got = append(got, img)
	}
	sort.Strings(got)

	want := []string{
		"ghcr.io/kubedb/postgres-restic-plugin:v0.29.0_16.4",
		"ghcr.io/kubedb/postgres-restic-plugin:v0.29.0_17.2",
		"ghcr.io/kubedb/postgres-restic-plugin:v0.29.0_18.2",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d images, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("image[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExpandVersionedImage(t *testing.T) {
	cases := []struct {
		name  string
		image string
		obj   map[string]any
		want  []string
	}{
		{
			name:  "no placeholder returned unchanged",
			image: "ghcr.io/kubedb/postgres-restic-plugin:v0.29.0",
			obj:   map[string]any{"availableVersions": []any{"16.4"}},
			want:  []string{"ghcr.io/kubedb/postgres-restic-plugin:v0.29.0"},
		},
		{
			name:  "placeholder but no availableVersions kept as-is",
			image: "repo/img:v1_${DB_VERSION}",
			obj:   map[string]any{},
			want:  []string{"repo/img:v1_${DB_VERSION}"},
		},
		{
			name:  "generic placeholder expanded",
			image: "repo/img:${VER}",
			obj:   map[string]any{"availableVersions": []any{"1.0", "2.0"}},
			want:  []string{"repo/img:1.0", "repo/img:2.0"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandVersionedImage(tc.image, tc.obj)
			sort.Strings(got)
			sort.Strings(tc.want)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
