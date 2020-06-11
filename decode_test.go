package bmp

import (
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"os"
	"testing"
)

const testDataDir = "./testdata"

func loadBMP(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, err := Decode(f)
	return img, err
}

var expectedImages = map[string]image.Image{
	"sample.bmp": &image.Paletted{
		Pix: []uint8{1, 0, 1, 0, 0, 1, 0, 1, 0, 0, 1, 0, 1, 0, 0, 1, 0, 1, 0, 0, 1, 0, 1, 0},
		Palette: color.Palette{
			color.RGBA{0x00, 0x00, 0x00, 0xff},
			color.RGBA{0xff, 0xff, 0xff, 0xff},
		},
	},
}

func checkPix(acctual, expected []uint8) error {
	sa := sha256.Sum256(acctual)
	se := sha256.Sum256(expected)
	if sa != se {
		return errors.New("bmp: incorrect pixels")
	}

	return nil
}

var fileNames = []string{
	"sample.bmp",
}

func TestDecode(t *testing.T) {
	for _, fname := range fileNames {
		img, err := loadBMP("testdata/" + fname)
		if err != nil {
			t.Errorf("%s", err)
			continue
		}

		switch img := img.(type) {
		case *image.Paletted:
			expected := expectedImages[fname].(*image.Paletted)

			if err := checkPix(img.Pix, expected.Pix); err != nil {
				t.Errorf("(testdata/%s) %s; acctual = %v, expected = %v", fname, err, img.Pix, expected.Pix)
			}

		default:
			t.Errorf("unexpected image type")
		}
	}
}
