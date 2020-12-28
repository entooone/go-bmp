package bmp

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
)

type decoder struct {
	r        io.Reader
	image    image.Image
	config   image.Config
	tmp      [3 * 256]byte
	topDown  bool
	bpp      int
	numColor int
	width    int
	height   int
}

func (d *decoder) readHeader() error {
	const (
		fileHeaderLen = 14
		infoHeaderLen = 40
	)

	// read file header and DIB header length
	if _, err := io.ReadFull(d.r, d.tmp[:fileHeaderLen+4]); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}

		return err
	}

	if string(d.tmp[:2]) != "BM" {
		return fmt.Errorf("bmp: invalid file signature (got: %q)", d.tmp[:2])
	}

	dibLen := binary.LittleEndian.Uint32(d.tmp[fileHeaderLen : fileHeaderLen+4])
	switch dibLen {
	// support these DIB header length
	case 40, 52, 60, 96, 108, 112, 120, 124:
	default:
		return fmt.Errorf("bmp: unsupported DIB header length (got: %d)", dibLen)
	}

	if _, err := io.ReadFull(d.r, d.tmp[fileHeaderLen+4:fileHeaderLen+dibLen]); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}

		return err
	}

	d.width = int(int32(binary.LittleEndian.Uint32(d.tmp[18:22])))
	d.height = int(int32(binary.LittleEndian.Uint32(d.tmp[22:26])))

	if d.height < 0 {
		d.height, d.topDown = -d.height, true
	}

	if d.width <= 0 || d.height == 0 {
		return fmt.Errorf("bmp: width must be greater than zero and height must be non-zero (width: %d, height: %d)", d.width, d.height)
	}

	d.bpp = int(binary.LittleEndian.Uint16(d.tmp[28:30]))
	compression := binary.LittleEndian.Uint16(d.tmp[30:34])

	rm, gm, bm := binary.LittleEndian.Uint32(d.tmp[54:58]), binary.LittleEndian.Uint32(d.tmp[58:62]), binary.LittleEndian.Uint32(d.tmp[62:66])
	mask := rm ^ gm ^ bm
	if compression == 3 && dibLen > infoHeaderLen &&
		(d.bpp == 16 && mask == 0x7fff) ||
		(d.bpp == 32 && mask == 0x00ffffff) {

		compression = 0
	}

	if compression != 0 {
		return fmt.Errorf("bmp: unsupported compression method (got: %d)", compression)
	}

	offset := binary.LittleEndian.Uint32(d.tmp[10:14])
	d.numColor = int(binary.LittleEndian.Uint32(d.tmp[46:50]))

	switch d.bpp {
	case 1, 4, 8:
		if int(offset) != fileHeaderLen+int(dibLen)+d.numColor*4 {
			return fmt.Errorf("bmp: incorrect offset (got: %d)", offset)
		}
	case 16, 24, 32:
		if int(offset) != fileHeaderLen+int(dibLen) {
			return fmt.Errorf("bmp: incorrect offset (got: %d)", offset)
		}
	default:
		return fmt.Errorf("bmp: unsupported the number of bits per pixel (got: %d)", d.bpp)
	}

	return nil
}

func (d *decoder) decodeConfig() error {
	if err := d.readHeader(); err != nil {
		return err
	}

	var model color.Model

	switch d.bpp {
	case 1, 4, 8:
		if _, err := io.ReadFull(d.r, d.tmp[:d.numColor*4]); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}

			return err
		}

		colorTable := make(color.Palette, d.numColor)
		for i := range colorTable {
			// BGR order
			colorTable[i] = color.RGBA{d.tmp[4*i+2], d.tmp[4*i+1], d.tmp[4*i], 0xff}
		}
		model = colorTable
	case 16, 24, 32:
		model = color.RGBAModel
	}

	d.config = image.Config{ColorModel: model, Width: d.width, Height: d.height}

	return nil
}

func (d *decoder) decodePalleted() error {
	paletted := image.NewPaletted(image.Rect(0, 0, d.width, d.height), d.config.ColorModel.(color.Palette))

	y0, y1, dy := d.height-1, -1, -1
	if d.topDown {
		y0, y1, dy = 0, d.height, 1
	}

	for y := y0; y != y1; y += dy {
		// row data must be an integer multiple of 4 bytes
		if _, err := io.ReadFull(d.r, d.tmp[:(d.width*d.bpp+31)/32*4]); err != nil {
			if err == io.EOF {
				return io.ErrUnexpectedEOF
			}
			return err
		}

		p := paletted.Pix[y*paletted.Stride : (y+1)*paletted.Stride]

		if d.width < 8/d.bpp {
			for j := 0; j < d.width; j++ {
				p[j] = (d.tmp[0] & (0xff &^ (0xff >> d.bpp) >> (d.bpp * j))) >> (8 - (d.bpp * (j + 1)))
			}
			continue
		}

		for i := 0; i < ((d.width+1)*d.bpp)/8; i++ {
			// e.g. d.bpp = 4:
			// j=0 => p[i*2] = (d.tmp[i] & 0xf0) >> 4
			// j=1 => p[i*2+1] = d.tmp[i] & 0xf
			for j := 0; j < (8 / d.bpp); j++ {
				p[i*2+j] = (d.tmp[i] & (0xff &^ (0xff >> d.bpp) >> (d.bpp * j))) >> (8 - (d.bpp * (j + 1)))
			}
		}
	}

	d.image = paletted

	return nil
}

func (d *decoder) decode16() error {
	rgba := image.NewRGBA(image.Rect(0, 0, d.width, d.height))

	y0, y1, dy := d.height-1, -1, -1
	if d.topDown {
		y0, y1, dy = 0, d.height, 1
	}

	for y := y0; y != y1; y += dy {
		if _, err := io.ReadFull(d.r, d.tmp[:(d.width*2+3)&^3]); err != nil {
			if err == io.EOF {
				return io.ErrUnexpectedEOF
			}
			return err
		}

		p := rgba.Pix[y*rgba.Stride : (y+1)*rgba.Stride]

		for i, j := 0, 0; i < d.width*4; i, j = i+4, j+2 {
			// BGR order
			p[i] = (d.tmp[j+1] & 0x3e) >> 1
			p[i+1] = ((d.tmp[j] & 0x07) << 3) | ((d.tmp[j+1] & 0xc0) >> 6)
			p[i+2] = (d.tmp[j] & 0xf8) >> 3
			p[i+3] = 0xff
		}
	}

	return nil
}

func (d *decoder) decode24() error {
	rgba := image.NewRGBA(image.Rect(0, 0, d.width, d.height))

	y0, y1, dy := d.height-1, -1, -1
	if d.topDown {
		y0, y1, dy = 0, d.height, 1
	}

	for y := y0; y != y1; y += dy {
		if _, err := io.ReadFull(d.r, d.tmp[:(d.width*3+3)&^3]); err != nil {
			if err == io.EOF {
				return io.ErrUnexpectedEOF
			}
			return err
		}

		p := rgba.Pix[y*rgba.Stride : (y+1)*rgba.Stride]

		for i, j := 0, 0; i < d.width*4; i, j = i+4, j+3 {
			// BGR order
			p[i] = d.tmp[j+2]
			p[i+1] = d.tmp[j+1]
			p[i+2] = d.tmp[j]
			p[i+3] = 0xff
		}
	}

	return nil
}

func (d *decoder) decode32() error {
	rgba := image.NewNRGBA(image.Rect(0, 0, d.width, d.height))

	y0, y1, dy := d.height-1, -1, -1
	if d.topDown {
		y0, y1, dy = 0, d.height, 1
	}

	for y := y0; y != y1; y += dy {
		p := rgba.Pix[y*rgba.Stride : (y+1)*rgba.Stride]

		if _, err := io.ReadFull(d.r, p[y*rgba.Stride:y*(rgba.Stride+1)]); err != nil {
			if err == io.EOF {
				return io.ErrUnexpectedEOF
			}
			return err
		}

		for i := 0; i < d.width*4; i++ {
			// BGRA order
			p[i], p[i+2] = p[i+2], p[i]
		}
	}

	return nil
}

func (d *decoder) decode() error {
	if err := d.decodeConfig(); err != nil {
		return err
	}

	var err error
	switch d.bpp {
	case 1, 4, 8:
		err = d.decodePalleted()
	case 16:
		err = d.decode16()
	case 24:
		err = d.decode24()
	case 32:
		err = d.decode32()
	}

	return err
}

// Decode reads a BMP image form io.Reader and returns an image.Image
func Decode(r io.Reader) (image.Image, error) {
	d := &decoder{
		r: r,
	}

	if err := d.decode(); err != nil {
		return nil, err
	}

	return d.image, nil
}

// DecodeConfig reads a BMP image from io.Reader and returns an image.Config
func DecodeConfig(r io.Reader) (image.Config, error) {
	d := &decoder{
		r: r,
	}

	if err := d.decodeConfig(); err != nil {
		return image.Config{}, err
	}

	return d.config, nil
}

func init() {
	image.RegisterFormat("bmp", "BM", Decode, DecodeConfig)
}
