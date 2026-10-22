package interchange

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestSchedulerExportMigrationSignatures(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migration directory")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "db", "migrations")
	files, err := filepath.Glob(filepath.Join(root, "020*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("migration inventory: %v", err)
	}
	created := map[string]string{}
	createPattern := regexp.MustCompile(`CREATE FUNCTION (photo\.[a-zA-Z0-9_]+)\(`)
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sql := string(body)
		for _, match := range createPattern.FindAllStringSubmatch(sql, -1) {
			name := match[1]
			create := sqlFunctionSignature(sql, "CREATE FUNCTION "+name, true)
			if create == "" && !strings.Contains(sql, "CREATE FUNCTION "+name+"()") {
				t.Fatalf("%s: missing CREATE signature for %s", path, name)
			}
			for _, prefix := range []string{
				"ALTER FUNCTION ", "REVOKE ALL ON FUNCTION ",
			} {
				actual := sqlFunctionSignature(sql, prefix+name, false)
				if (actual == "" && !strings.Contains(sql, prefix+name+"()")) || actual != create {
					t.Errorf("%s: %s signature differs from CREATE", path, prefix+name)
				}
			}
			if strings.Contains(sql, "GRANT EXECUTE ON FUNCTION "+name+"(") {
				actual := sqlFunctionSignature(sql, "GRANT EXECUTE ON FUNCTION "+name, false)
				if actual != create {
					t.Errorf("%s: GRANT signature differs from CREATE for %s", path, name)
				}
			}
			created[name+"("+create+")"] = path
		}
	}
	for _, required := range schedulerExportSQLFunctions {
		if strings.HasPrefix(required, "photo.v30_export_adoption_") {
			if _, ok := created[required]; !ok {
				t.Errorf("scheduler startup signature has no matching migration: %s", required)
			}
		}
	}
}

func sqlFunctionSignature(sql, prefix string, declaration bool) string {
	start := strings.Index(sql, prefix+"(")
	if start < 0 {
		return ""
	}
	start += len(prefix) + 1
	end := strings.IndexByte(sql[start:], ')')
	if end < 0 {
		return ""
	}
	raw := sql[start : start+end]
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	parts := strings.Split(raw, ",")
	for index, part := range parts {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			return ""
		}
		if declaration {
			parts[index] = fields[len(fields)-1]
		} else {
			parts[index] = strings.Join(fields, "")
		}
	}
	return strings.Join(parts, ",")
}
