package common

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsNilRecognizesNilableValuesWithoutPanicking(t *testing.T) {
	var pointer *int
	var channel chan int
	var function func()
	var interfaceValue io.Reader = (*nilReader)(nil)
	var mapping map[string]int
	var slice []int

	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{name: "nil interface", value: nil, want: true},
		{name: "typed nil pointer", value: pointer, want: true},
		{name: "typed nil channel", value: channel, want: true},
		{name: "typed nil function", value: function, want: true},
		{name: "typed nil interface implementation", value: interfaceValue, want: true},
		{name: "typed nil map", value: mapping, want: true},
		{name: "typed nil slice", value: slice, want: true},
		{name: "non-nil pointer", value: new(int)},
		{name: "non-nil channel", value: make(chan int)},
		{name: "non-nil function", value: func() {}},
		{name: "non-nil map", value: map[string]int{}},
		{name: "non-nil slice", value: []int{}},
		{name: "non-nilable integer", value: 0},
		{name: "non-nilable struct", value: struct{}{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, IsNil(test.value))
		})
	}
}

type nilReader struct{}

func (*nilReader) Read([]byte) (int, error) { return 0, io.EOF }
