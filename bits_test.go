package nebula

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBits(t *testing.T) {
	b := NewBits(10)

	// make sure it is the right size
	assert.Len(t, b.bits, 10)

	// This is initialized to zero - receive one. This should work.

	assert.True(t, b.Check(1))
	u := b.Update(1)
	assert.True(t, u)
	assert.EqualValues(t, 1, b.current)
	g := []bool{false, true, false, false, false, false, false, false, false, false}
	assert.Equal(t, g, b.bits)

	// Receive two
	assert.True(t, b.Check(2))
	u = b.Update(2)
	assert.True(t, u)
	assert.EqualValues(t, 2, b.current)
	g = []bool{false, true, true, false, false, false, false, false, false, false}
	assert.Equal(t, g, b.bits)

	// Receive two again - it will fail
	assert.False(t, b.Check(2))
	u = b.Update(2)
	assert.False(t, u)
	assert.EqualValues(t, 2, b.current)

	// Jump ahead to 15, which should clear everything and set the 6th element
	assert.True(t, b.Check(15))
	u = b.Update(15)
	assert.True(t, u)
	assert.EqualValues(t, 15, b.current)
	g = []bool{false, false, false, false, false, true, false, false, false, false}
	assert.Equal(t, g, b.bits)

	// Mark 14, which is allowed because it is in the window
	assert.True(t, b.Check(14))
	u = b.Update(14)
	assert.True(t, u)
	assert.EqualValues(t, 15, b.current)
	g = []bool{false, false, false, false, true, true, false, false, false, false}
	assert.Equal(t, g, b.bits)

	// Mark 5, which is not allowed because it is not in the window
	assert.False(t, b.Check(5))
	u = b.Update(5)
	assert.False(t, u)
	assert.EqualValues(t, 15, b.current)
	g = []bool{false, false, false, false, true, true, false, false, false, false}
	assert.Equal(t, g, b.bits)

	// make sure we handle wrapping around once to the current position
	b = NewBits(10)
	assert.True(t, b.Update(1))
	assert.True(t, b.Update(11))
	assert.Equal(t, []bool{false, true, false, false, false, false, false, false, false, false}, b.bits)

	// Walk through a few windows in order
	b = NewBits(10)
	for i := uint64(0); i <= 100; i++ {
		assert.True(t, b.Check(i), "Error while checking %v", i)
		assert.True(t, b.Update(i), "Error while updating %v", i)
	}
}

func TestBitsDupeCounter(t *testing.T) {
	b := NewBits(10)
	b.lostCounter.Clear()
	b.dupeCounter.Clear()
	b.outOfWindowCounter.Clear()

	assert.True(t, b.Update(1))
	assert.Equal(t, int64(0), b.dupeCounter.Count())

	assert.False(t, b.Update(1))
	assert.Equal(t, int64(1), b.dupeCounter.Count())

	assert.True(t, b.Update(2))
	assert.Equal(t, int64(1), b.dupeCounter.Count())

	assert.True(t, b.Update(3))
	assert.Equal(t, int64(1), b.dupeCounter.Count())

	assert.False(t, b.Update(1))
	assert.Equal(t, int64(0), b.lostCounter.Count())
	assert.Equal(t, int64(2), b.dupeCounter.Count())
	assert.Equal(t, int64(0), b.outOfWindowCounter.Count())
}

func TestBitsOutOfWindowCounter(t *testing.T) {
	b := NewBits(10)
	b.lostCounter.Clear()
	b.dupeCounter.Clear()
	b.outOfWindowCounter.Clear()

	assert.True(t, b.Update(20))
	assert.Equal(t, int64(0), b.outOfWindowCounter.Count())

	assert.True(t, b.Update(21))
	assert.True(t, b.Update(22))
	assert.True(t, b.Update(23))
	assert.True(t, b.Update(24))
	assert.True(t, b.Update(25))
	assert.True(t, b.Update(26))
	assert.True(t, b.Update(27))
	assert.True(t, b.Update(28))
	assert.True(t, b.Update(29))
	assert.Equal(t, int64(0), b.outOfWindowCounter.Count())

	assert.False(t, b.Update(0))
	assert.Equal(t, int64(1), b.outOfWindowCounter.Count())

	//tODO: make sure lostcounter doesn't increase in orderly increment
	assert.Equal(t, int64(20), b.lostCounter.Count())
	assert.Equal(t, int64(0), b.dupeCounter.Count())
	assert.Equal(t, int64(1), b.outOfWindowCounter.Count())
}

func TestBitsLostCounter(t *testing.T) {
	b := NewBits(10)
	b.lostCounter.Clear()
	b.dupeCounter.Clear()
	b.outOfWindowCounter.Clear()

	//assert.True(t, b.Update(0))
	assert.True(t, b.Update(0))
	assert.True(t, b.Update(20))
	assert.True(t, b.Update(21))
	assert.True(t, b.Update(22))
	assert.True(t, b.Update(23))
	assert.True(t, b.Update(24))
	assert.True(t, b.Update(25))
	assert.True(t, b.Update(26))
	assert.True(t, b.Update(27))
	assert.True(t, b.Update(28))
	assert.True(t, b.Update(29))
	assert.Equal(t, int64(20), b.lostCounter.Count())
	assert.Equal(t, int64(0), b.dupeCounter.Count())
	assert.Equal(t, int64(0), b.outOfWindowCounter.Count())

	b = NewBits(10)
	b.lostCounter.Clear()
	b.dupeCounter.Clear()
	b.outOfWindowCounter.Clear()

	assert.True(t, b.Update(0))
	assert.Equal(t, int64(0), b.lostCounter.Count())
	assert.True(t, b.Update(9))
	assert.Equal(t, int64(0), b.lostCounter.Count())
	// 10 will set 0 index, 0 was already set, no lost packets
	assert.True(t, b.Update(10))
	assert.Equal(t, int64(0), b.lostCounter.Count())
	// 11 will set 1 index, 1 was missed, we should see 1 packet lost
	assert.True(t, b.Update(11))
	assert.Equal(t, int64(1), b.lostCounter.Count())
	// Now let's fill in the window, should end up with 8 lost packets
	assert.True(t, b.Update(12))
	assert.True(t, b.Update(13))
	assert.True(t, b.Update(14))
	assert.True(t, b.Update(15))
	assert.True(t, b.Update(16))
	assert.True(t, b.Update(17))
	assert.True(t, b.Update(18))
	assert.True(t, b.Update(19))
	assert.Equal(t, int64(8), b.lostCounter.Count())

	// Jump ahead by a window size
	assert.True(t, b.Update(29))
	assert.Equal(t, int64(8), b.lostCounter.Count())
	// Now lets walk ahead normally through the window, the missed packets should fill in
	assert.True(t, b.Update(30))
	assert.True(t, b.Update(31))
	assert.True(t, b.Update(32))
	assert.True(t, b.Update(33))
	assert.True(t, b.Update(34))
	assert.True(t, b.Update(35))
	assert.True(t, b.Update(36))
	assert.True(t, b.Update(37))
	assert.True(t, b.Update(38))
	// 39 packets tracked, 22 seen, 17 lost
	assert.Equal(t, int64(17), b.lostCounter.Count())

	// Jump ahead by 2 windows, should have recording 1 full window missing
	assert.True(t, b.Update(58))
	assert.Equal(t, int64(27), b.lostCounter.Count())
	// Now lets walk ahead normally through the window, the missed packets should fill in from this window
	assert.True(t, b.Update(59))
	assert.True(t, b.Update(60))
	assert.True(t, b.Update(61))
	assert.True(t, b.Update(62))
	assert.True(t, b.Update(63))
	assert.True(t, b.Update(64))
	assert.True(t, b.Update(65))
	assert.True(t, b.Update(66))
	assert.True(t, b.Update(67))
	// 68 packets tracked, 32 seen, 36 missed
	assert.Equal(t, int64(36), b.lostCounter.Count())
	assert.Equal(t, int64(0), b.dupeCounter.Count())
	assert.Equal(t, int64(0), b.outOfWindowCounter.Count())
}

func BenchmarkBits(b *testing.B) {
	z := NewBits(10)
	for n := 0; n < b.N; n++ {
		for i := range z.bits {
			z.bits[i] = true
		}
		for i := range z.bits {
			z.bits[i] = false
		}

	}
}
