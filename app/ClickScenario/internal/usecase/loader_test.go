package usecase

import (
	"reflect"
	"testing"
)

func TestParseAuthors(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []int64
		wantErr bool
	}{
		{
			name: "простой список через запятую",
			in:   "1, 2, 3",
			want: []int64{1, 2, 3},
		},
		{
			name: "квадратные скобки",
			in:   "[1, 2, 3]",
			want: []int64{1, 2, 3},
		},
		{
			name: "разделение пробелами",
			in:   "1 2 3",
			want: []int64{1, 2, 3},
		},
		{
			name: "пустая строка — пустой слайс без ошибки",
			in:   "",
			want: []int64{},
		},
		{
			name:    "нечисловое значение",
			in:      "1, x, 3",
			wantErr: true,
		},
		{
			name: "смешанные разделители и лишние пробелы",
			in:   "  1,2 ,  3,4 ",
			want: []int64{1, 2, 3, 4},
		},
		{
			name: "скобки и пробелы вместе",
			in:   "[ 1 , 2 , 3 ]",
			want: []int64{1, 2, 3},
		},
		{
			name: "повторы допустимы",
			in:   "5, 5, 5",
			want: []int64{5, 5, 5},
		},
		{
			name: "ведущие нули и большие числа",
			in:   "007, 100, 9223372036854775807",
			want: []int64{7, 100, 9223372036854775807},
		},
		{
			name:    "переполнение int64",
			in:      "99999999999999999999",
			wantErr: true,
		},
		{
			name: "только скобки — пустой слайс",
			in:   "[]",
			want: []int64{},
		},
		{
			name: "только разделители — пустой слайс",
			in:   " , , ",
			want: []int64{},
		},
		{
			name:    "отрицательные значения недопускаются",
			in:      "-1, -2",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAuthors(tt.in)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ожидалась ошибка, но её нет; получено %v", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}

			// reflect.DeepEqual различает nil и пустой слайс, поэтому
			// нормализуем nil → []int64{}, чтобы тест не зависел от
			// внутренней реализации make(...,0,n) vs nil.
			if got == nil {
				got = []int64{}
			}
			if tt.want == nil {
				tt.want = []int64{}
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseAuthors(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
