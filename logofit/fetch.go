package logofit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Families are the candidate Google Fonts families.
var Families = []string{"Montserrat", "Poppins", "Lexend", "Outfit", "Red Hat Display", "Urbanist",
	"Plus Jakarta Sans", "Manrope", "Sora", "Figtree", "Raleway", "Nunito Sans", "Kanit", "Archivo",
	"Work Sans", "Inter", "Rubik", "Mulish"}

// Weights are the weights tried per family.
var Weights = []int{700, 800, 900}

// FontFile is the on-disk name of a family/weight.
func FontFile(family string, weight int) string {
	return fmt.Sprintf("%s-%d.ttf", strings.ReplaceAll(family, " ", ""), weight)
}

var (
	reFace = regexp.MustCompile(`(?s)(?:/\*\s*([\w-]+)\s*\*/\s*)?@font-face\s*\{(.*?)\}`)
	reURL  = regexp.MustCompile(`url\((https://[^)]+)\)`)
)

// fvar reports whether a TTF/OTF table directory has an fvar table.
func hasFvar(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	n := int(data[4])<<8 | int(data[5])
	for i := 0; i < n && 12+16*i+4 <= len(data); i++ {
		if string(data[12+16*i:12+16*i+4]) == "fvar" {
			return true
		}
	}
	return false
}

func get(c *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "logofit/0.1")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return data, nil
}

// Fetch downloads every candidate into dir and writes dir/SOURCES.md
// (licence, source URL, SHA-256, variable-font flag, failures).
func Fetch(dir string, log io.Writer) error {
	return fetchFrom(&http.Client{Timeout: 60 * time.Second}, "https://fonts.googleapis.com/css2", dir, log)
}

// fetchFrom is Fetch against the CSS endpoint cssBase with client c.
func fetchFrom(c *http.Client, cssBase, dir string, log io.Writer) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var md bytes.Buffer
	md.WriteString("# Font sources\n\nAll families are Google Fonts, licensed under the SIL Open Font License 1.1\n" +
		"(licence taken from the Google Fonts catalogue, not re-read from each file's name table).\n" +
		"Files are fetched by `logofit fetch` and are not committed (see .gitignore).\n\n" +
		"| file | family | weight | source URL | SHA-256 | fvar |\n|---|---|---|---|---|---|\n")
	var failed []string
	for _, fam := range Families {
		for _, w := range Weights {
			name := FontFile(fam, w)
			cssURL := fmt.Sprintf("%s?family=%s:wght@%d", cssBase, url.QueryEscape(fam), w)
			css, err := get(c, cssURL)
			if err != nil {
				failed = append(failed, fmt.Sprintf("%s %d: css %v", fam, w, err))
				continue
			}
			var src string
			for _, m := range reFace.FindAllStringSubmatch(string(css), -1) {
				u := reURL.FindStringSubmatch(m[2])
				if u == nil {
					continue
				}
				if src == "" || m[1] == "latin" {
					src = u[1]
				}
			}
			if src == "" {
				failed = append(failed, fmt.Sprintf("%s %d: no font URL in css", fam, w))
				continue
			}
			data, err := get(c, src)
			if err != nil {
				failed = append(failed, fmt.Sprintf("%s %d: %v", fam, w, err))
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			fmt.Fprintf(&md, "| %s | %s | %d | %s | `%s` | %v |\n", name, fam, w, src, hex.EncodeToString(sum[:]), hasFvar(data))
			fmt.Fprintf(log, "ok   %s (%d bytes)\n", name, len(data))
		}
	}
	if len(failed) > 0 {
		md.WriteString("\n## Not fetched\n\n")
		for _, f := range failed {
			md.WriteString("- " + f + "\n")
			fmt.Fprintf(log, "FAIL %s\n", f)
		}
	}
	return os.WriteFile(filepath.Join(dir, "SOURCES.md"), md.Bytes(), 0o644)
}
