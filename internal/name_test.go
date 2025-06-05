package internal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateDBName(t *testing.T) {
	tests := []struct {
		name     string
		testPath string
		want     string
	}{
		{
			name:     "simple path",
			testPath: "example.com/pkg.TestFunc/TestFunc",
			want:     "_sqlpt_8y4IYUitLKFNFeblsONxirTXVcFAgsuJ6r7nVVhp_jPVnM5t9aFQo_VR",
		},
		{
			name:     "path with special characters",
			testPath: "example.com/pkg.TestFunc/Test-Case_With@Special#Chars",
			want:     "_sqlpt_EM9kCpQrAp23hlAwb5EFzH7OJVA6VO380Cmm7ktKu1Ir44akQ63YLDlY",
		},
		{
			name:     "long path",
			testPath: "example.com/very/very/long/path/with/many/segments/pkg.TestVeryLongTestName.func1/TestVeryLongTestName/Test_Case_With_Very_Long_Name",
			want:     "_sqlpt_pgMdTcabDMxTVMCBFmPu1pcZUOCO87DvzuuWpEULdFzomd3HJsC9G3oD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateDBName(tt.testPath)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, 63, len(got), "database name should be exactly 63 characters")
			assert.True(t, strings.HasPrefix(got, "_sqlpt_"), "database name should start with _sqlpt_")
			assert.False(t, strings.Contains(got, "-"), "database name should not contain hyphens")

			got2 := GenerateDBName(tt.testPath)
			assert.Equal(t, got, got2, "GenerateDBName should be deterministic")
		})
	}
}

func TestGetTestPath(t *testing.T) {
	path := GetTestPath(t)
	assert.Equal(t, "github.com/ciffelia/sqlpt/internal.TestGetTestPath/TestGetTestPath", path)

	path = testGetTestPathHelper(t)
	assert.Equal(t, "github.com/ciffelia/sqlpt/internal.TestGetTestPath/TestGetTestPath", path)

	t.Run("subtest 1", func(t *testing.T) {
		path := GetTestPath(t)
		assert.Equal(t, "github.com/ciffelia/sqlpt/internal.TestGetTestPath.func1/TestGetTestPath/subtest_1", path)

		t.Run("subsubtest 1", func(t *testing.T) {
			path := GetTestPath(t)
			assert.Equal(t, "github.com/ciffelia/sqlpt/internal.TestGetTestPath.func1.1/TestGetTestPath/subtest_1/subsubtest_1", path)
		})
	})
}

//go:noinline
func testGetTestPathHelper(t *testing.T) string {
	t.Helper()
	return GetTestPath(t)
}
