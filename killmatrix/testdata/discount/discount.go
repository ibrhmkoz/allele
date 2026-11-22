// Package discount is the example from the roadmap's kill matrix.
package discount

func Discount(price int, vip bool) int {
	if price > 100 && vip {
		return price - 20
	}
	return price
}
