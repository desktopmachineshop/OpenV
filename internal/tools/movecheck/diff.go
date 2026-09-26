package main

import "fmt"

// unifiedDiff returns a unified diff of two line lists, with three lines of
// context, or nothing when they are equal. It is a plain longest-common-
// subsequence diff: a flattened main() is a few thousand lines at most.
func unifiedDiff(a, b []string, aName, bName string) []string {
	n, m := len(a), len(b)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	type op struct {
		kind   byte
		line   string
		ai, bi int
	}
	var ops []op
	var changed []int
	for i, j := 0, 0; i < n || j < m; {
		switch {
		case i < n && j < m && a[i] == b[j]:
			ops = append(ops, op{' ', a[i], i, j})
			i, j = i+1, j+1
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			changed = append(changed, len(ops))
			ops = append(ops, op{'-', a[i], i, j})
			i++
		default:
			changed = append(changed, len(ops))
			ops = append(ops, op{'+', b[j], i, j})
			j++
		}
	}
	if len(changed) == 0 {
		return nil
	}
	const context = 3
	out := []string{"--- " + aName, "+++ " + bName}
	for k := 0; k < len(changed); {
		start := max(changed[k]-context, 0)
		last := k
		for last+1 < len(changed) && changed[last+1]-changed[last] <= 2*context {
			last++
		}
		end := min(changed[last]+context+1, len(ops))
		aLen, bLen := 0, 0
		for _, o := range ops[start:end] {
			if o.kind != '+' {
				aLen++
			}
			if o.kind != '-' {
				bLen++
			}
		}
		out = append(out, fmt.Sprintf("@@ -%d,%d +%d,%d @@", ops[start].ai+1, aLen, ops[start].bi+1, bLen))
		for _, o := range ops[start:end] {
			out = append(out, string(o.kind)+o.line)
		}
		k = last + 1
	}
	return out
}
