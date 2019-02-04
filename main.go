package main

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"path"
	"strings"
	"mime"

	getopt "github.com/pborman/getopt"
)

var exclude = getopt.ListLong("exclude", 'x', "", "glob patterns to exclude")
var help = getopt.BoolLong("help", 'h', "", "print this help")

func main() {
	getopt.SetParameters("<root dir> <bucket name>")
	getopt.Parse()
	if *help {
		getopt.PrintUsage(os.Stdout)
		return
	}

	args := getopt.Args()
	if len(args) != 2 {
		getopt.PrintUsage(os.Stderr)
		os.Exit(1)
	}

	rootDir := args[0]
	bucketName := args[1]

	resourcesMap := map[string]interface{}{}
	result := map[string]interface{}{
		"resource": map[string]interface{}{
			"aws_s3_bucket_object": resourcesMap,
		},
	}

	filepath.Walk(rootDir, func(subpath string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %s\n", subpath, err)
			// Skip stuff we can't read.
			return nil
		}

		relPath, err := filepath.Rel(rootDir, subpath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed make %s relative: %s\n", subpath, err)
			return nil
		}

		subpath, err = filepath.EvalSymlinks(subpath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to resolve symlink %s: %s\n", subpath, err)
			return nil
		}

		if info.IsDir() {
			// Don't need to create directories since they are implied
			// by the files within.
			return nil
		}

		for _, pattern := range *exclude {
			var toMatch []string
			if strings.ContainsRune(pattern, filepath.Separator) {
				toMatch = append(toMatch, relPath)
			} else {
				// If the pattern does not include a subpath separator
				// then we apply it to all segments of the subpath
				// individually.
				toMatch = strings.Split(relPath, string(filepath.Separator))
			}

			for _, matchPath := range toMatch {
				matched, _ := filepath.Match(pattern, matchPath)
				if matched {
					return nil
				}
			}
		}

		// We use the initial bytes of the file to infer a MIME type
		file, err := os.Open(subpath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening %s: %s\n", subpath, err)
			return nil
		}
		statinfo, err := file.Stat()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stats %s: %s\n", subpath, err)
			return nil
		}
		hasher := sha1.New()
		fileBytes := make([]byte, 1024*1024)
		contentType := ""
		_, err = file.Read(fileBytes)
		// If we got back and error and it isn't the end of file then
		// skip it.  This does "something" with 0 length files.  It is
		// likely we should really be categorizing those based on file
		// extension.
		if err != nil && err != io.EOF {
			fmt.Fprintf(os.Stderr, "Error reading %s: %s\n", subpath, err)
			return nil
		}
		if strings.HasSuffix(relPath, ".svg") {
			// If we start to need a set of overrides for DetectContentType
			// then we need to find a different way to do this.
			contentType = "image/svg+xml"
		// } else if strings.HasSuffix(relPath, ".css") {
		// 	// If we start to need a set of overrides for DetectContentType
		// 	// then we need to find a different way to do this.
		// 	contentType = "text/css"
		// } else if strings.HasSuffix(relPath, ".js") {
		// 	// If we start to need a set of overrides for DetectContentType
		// 	// then we need to find a different way to do this.
		// 	contentType = "application/javascript"
		} else if mimetype := mime.TypeByExtension(path.Ext(subpath)); mimetype != "" {
			contentType = mimetype
		} else {
			contentType = http.DetectContentType(fileBytes)
		}

		// Resource name is a hash of the subpath, so it should stay consistent
		// for a given file subpath as long as the relative subpath to the target
		// directory is always the same across runs.
		hasher.Write([]byte(relPath))
		resourceName := fmt.Sprintf("%x", hasher.Sum(nil))

		resourcesMap[resourceName] = map[string]interface{}{
			"bucket":       bucketName,
			"key":          relPath,
			"source":       subpath,
			"etag":         fmt.Sprintf("${md5(file(%q))}", subpath),
			"content_type": contentType,
			"cache_control": "max-age=86400, stale-while-revalidate=3600",
			"last_modified": statinfo.ModTime().Format(http.TimeFormat),
		}

		return nil
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.Encode(result)
}
