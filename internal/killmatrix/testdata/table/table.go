// Package table has a table-driven test, so its matrix has a column per subtest.
package table

// Abs returns the absolute value of x.
func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
