package fixture

import "example.com/fixture/helper"

type Thing struct{}

func (Thing) Run()      {}
func (*Thing) Pointer() {}

type Number int

func Entry(flag bool, dynamic func(), thing Thing) {
	if flag {
		Recur()
	} else {
		helper.Work()
	}
	dynamic()
	thing.Run()
	Thing.Run(thing)
	(&thing).Pointer()
	_ = Number(1)
	_ = helper.Value(1)
	_ = int(2)
	_ = len([]int{1})
	_ = make([]int, 0)
	defer helper.Work()
	go helper.Work()
	func() { Hidden() }()
	_ = func() { Hidden() }
	Generic[int]()
}

func Recur() {
	Recur()
	Leaf()
}
func Leaf()           {}
func Hidden()         {}
func Generic[T any]() {}

func Shadow() {
	Leaf := func() {}
	Leaf()
}

func CycleA() { CycleB() }
func CycleB() { CycleA() }

func Unchecked() { missingSymbol() }
