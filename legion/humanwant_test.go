package main

import "testing"

func TestPriceNum(t *testing.T) {
	cases := map[[2]string]int{{"5", "k"}: 5000, {"5,000", ""}: 5000, {"1.5", "m"}: 1500000, {"300", "z"}: 300}
	for in, want := range cases {
		if got := priceNum(in[0], in[1]); got != want {
			t.Errorf("priceNum(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestParseBuyLineNotBuy(t *testing.T) {
	if _, _, ok := parseBuyLine("hello everyone"); ok {
		t.Error("plain chat parsed as a buy line")
	}
}
