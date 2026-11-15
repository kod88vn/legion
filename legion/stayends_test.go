package main

import "testing"

func TestStayOrderOver(t *testing.T) {
	if !stayOrderOver(5, 0) || !stayOrderOver(14, 10) || stayOrderOver(12, 15) || stayOrderOver(20, 0) {
		t.Fatal("stay ends within 6 cells or when she walks away; it holds while she comes closer")
	}
}
