package main
import("sort";"regexp")
type outsideDiffError struct{Tables []string}
func(e *outsideDiffError)Error()string{return "OUTSIDE_PROTECTED_DIFF"}
func protectedDiff(before,after snapshot)error{keys:=map[string]bool{};for k:=range before{keys[k]=true};for k:=range after{keys[k]=true};var tables []string;for k:=range keys{a,ok1:=before[k];b,ok2:=after[k];if !ok1||!ok2||a!=b{if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(k){k="invalid_table_identifier"};tables=append(tables,k)}};if len(tables)==0{return nil};sort.Strings(tables);return &outsideDiffError{tables}}
