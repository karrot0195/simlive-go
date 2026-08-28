#!/bin/bash
cp -r $(find ./internal/ -type f) ./ai-sources/


# 2. Flatten files from ./cmd/ into ./ai-sources/ with renamed filenames (e.g., cmd_foo_bar.go)
find ./cmd -type f -name '*.go' | while read -r file; do
    # Transform ./cmd/sub/dir/main.go into sub_dir_main.go
    name=$(echo "$file" | sed 's#^\./cmd/##; s#/#_#g')
    cp "$file" "./ai-sources/$name"
done