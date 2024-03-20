package edid

import (
	"embed"
	"fmt"
	"math"

	"github.com/wader/fq/format"
	"github.com/wader/fq/pkg/decode"
	"github.com/wader/fq/pkg/interp"
	"github.com/wader/fq/pkg/scalar"
	"golang.org/x/text/encoding"
)

//go:embed edid.md
var edidFS embed.FS

var edidExtensionGroup decode.Group

func init() {
	interp.RegisterFormat(
		format.EDID,
		&decode.Format{
			Description: "Extended Display Identification Data",
			Groups:      []*decode.Group{format.Probe},
			DecodeFn:    decodeEDID,
			Dependencies: []decode.Dependency{
				{Groups: []*decode.Group{format.EDID_Extension}, Out: &edidExtensionGroup},
			},
		})
	interp.RegisterFS(edidFS)
}

type edidContext struct {
	revision uint64
	contFreq bool
}

// Create a manual Uint field with a source address
func FieldValueUintAddr(d *decode.D, name string, a uint64, firstBit int64, nBits int64, sms ...scalar.UintMapper) {
	d.FieldRangeFn(name, firstBit, nBits, func() *decode.Value {
		var err error = nil
		s := scalar.Uint{Actual: a, DisplayFormat: scalar.NumberDecimal}
		for _, sm := range sms {
			s, err = sm.MapUint(s)
			if err != nil {
				return &decode.Value{V: &s}
			}
		}
		return &decode.Value{V: &s}
	})
}

// Create a manual Flt field with a source address
func FieldValueFltAddr(d *decode.D, name string, a float64, firstBit int64, nBits int64, sms ...scalar.FltMapper) {
	d.FieldRangeFn(name, firstBit, nBits, func() *decode.Value {
		var err error = nil
		s := scalar.Flt{Actual: a}
		for _, sm := range sms {
			s, err = sm.MapFlt(s)
			if err != nil {
				return &decode.Value{V: &s}
			}
		}
		return &decode.Value{V: &s}
	})
}

func fiveBitLetter(code uint64) string {
	r := rune(64 + code) // ASCII "A" is 65. fiveBit code for "A" is 1.
	return string(r)
}

var manufacturerMapper = scalar.UintFn(func(s scalar.Uint) (scalar.Uint, error) {
	firstLetter := s.Actual >> 10 & 0x1f
	secondLetter := s.Actual >> 5 & 0x1f
	thirdLetter := s.Actual & 0x1f

	s.Sym = fmt.Sprintf("%s%s%s", fiveBitLetter(firstLetter), fiveBitLetter(secondLetter), fiveBitLetter(thirdLetter))

	return s, nil
})

// maps bit index in flag
var establishedTimingsIMapper = scalar.UintMapSymStr{
	0: "800x600@60Hz",
	1: "800x600@56Hz",
	2: "640x480@75Hz",
	3: "640x480@72Hz",
	4: "640x480@67Hz",
	5: "640x480@60Hz",
	6: "720x400@88Hz",
	7: "720x400@70Hz",
}

// maps bit index in flag
var establishedTimingsIIMapper = scalar.UintMapSymStr{
	0: "1280x1024@75Hz",
	1: "1024x768@75Hz",
	2: "1024x768@70Hz",
	3: "1024x768@60Hz",
	4: "1024x768@87Hz(I)",
	5: "832x624@75Hz",
	6: "800x600@75Hz",
	7: "800x600@72Hz",
}

// maps bit index in flag
var manufacturerTimingsMapper = scalar.UintMapSymStr{
	0: "reserved",
	1: "reserved",
	2: "reserved",
	3: "reserved",
	4: "reserved",
	5: "reserved",
	6: "reserved",
	7: "1152x870@75Hz",
}

var interfaceMapper = scalar.UintMapSymStr{
	0: "undefined",
	1: "dvi",
	2: "hdmia",
	3: "hdmib",
	4: "mddi",
	5: "displayport",
}

var bitDepthMapper = scalar.UintMap{
	0: {Sym: "undefined"},
	1: {Sym: 6, Description: "6 bits per color"},
	2: {Sym: 8, Description: "8 bits per color"},
	3: {Sym: 10, Description: "10 bits per color"},
	4: {Sym: 12, Description: "12 bits per color"},
	5: {Sym: 14, Description: "14 bits per color"},
	6: {Sym: 16, Description: "16 bits per color"},
	7: {Sym: "reserved"},
}

var yearMapper = scalar.UintFn(func(s scalar.Uint) (scalar.Uint, error) {
	s.Sym = s.Actual + 1990
	return s, nil
})

func mapTimings(d *decode.D, flags uint64, firstBit int64, sms ...scalar.UintMapper) {
	for i := 0; i < 8; i++ {
		if flags>>i&0x1 == 1 {
			FieldValueUintAddr(d, "timing", uint64(i), firstBit, 8, sms...)
		}
	}
}

func greatest_common_divisor(a, b uint64) uint64 {
	for b != 0 {
		t := b
		b = a % b
		a = t
	}
	return a
}

var aspectRatioEdgeCase = map[string]string{
	"8:5": "16:10",
	"5:8": "10:16",
	"7:3": "21:9",
	"3:7": "9:21",
}

func aspectRatioDenominator(width uint64, height uint64) string {
	gcd := greatest_common_divisor(width, height)
	nWidth := width / gcd
	nHeight := height / gcd

	try := fmt.Sprintf("%d:%d", nWidth, nHeight)
	edge, ok := aspectRatioEdgeCase[try]
	if ok {
		return edge
	}
	return try
}

func aspectRatio(aspect float64) string {
	for n := 1; n <= 20; n++ {
		m := uint64(aspect*float64(n) + 0.5)
		if math.Abs(aspect-float64(m)/float64(n)) < 0.01 {
			return aspectRatioDenominator(m, uint64(n))
		}
	}
	return fmt.Sprintf("%2f", aspect)
}

func decodeFileHeader(d *decode.D, ec *edidContext) {
	// byte 0-7
	d.FieldRawLen("identifier", 8*8, d.AssertBitBuf([]byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00})) // Magic

	// byte 8-9
	d.FieldU16BE("manufacturer_id", manufacturerMapper)

	// byte 10-11
	d.FieldU16("manufacturer_product_code")

	// byte 12-15
	d.FieldU32("serial_number")

	// byte 16
	week := d.FieldU8("week_of_manufacture")

	// byte 17
	if week == 0xff {
		d.FieldU8("year_of_model", yearMapper)
	} else {
		d.FieldU8("year_of_manufacture", yearMapper)
	}

	// byte 18
	d.FieldU8("edid_version")

	// byte 19
	ec.revision = d.FieldU8("edid_revision")
}

func decodeBasicDisplayParameters(d *decode.D, ec *edidContext) {
	// byte 20
	inputType := d.FieldU1("input_type", scalar.UintMapSymStr{0: "analog", 1: "digital"})
	if inputType == 1 { // digital
		d.FieldU3("bit_depth", bitDepthMapper)
		d.FieldU4("video_interface", interfaceMapper)
	} else { // analog
		d.FieldU2("white_sync_leves_relative_to_blank", scalar.UintMapDescription{
			0: "+0.7/−0.3 V",
			1: "+0.714/−0.286 V",
			2: "+1.0/−0.4 V",
			3: "+0.7/0 V (EVC)",
		})
		d.FieldBool("blank_to_black_setup_expected")
		d.FieldBool("separate_sync_supported")
		d.FieldBool("composite_sync_on_hsync_supported")
		d.FieldBool("sync_on_green_supported")
		d.FieldBool("serration_on_vsync_supported")
	}

	// byte 21
	hScreenSize := d.FieldU8("horizontal_screen_size", descUintMapper("cm"))

	// byte 22
	vScreenSize := d.FieldU8("vertical_screen_size", descUintMapper("cm"))

	if hScreenSize == 0 && vScreenSize == 0 {
		d.FieldValueStr("aspect_ratio", "undefined")
	} else {
		if hScreenSize == 0 { // portrait
			aspect := 100 / float64(vScreenSize+99)
			d.FieldValueStr("aspect_ratio", aspectRatio(aspect))
		} else if vScreenSize == 0 { // landscape
			aspect := float64(hScreenSize+99) / 100
			d.FieldValueStr("aspect_ratio", aspectRatio(aspect))
		} else {
			d.FieldValueStr("aspect_ratio", aspectRatio(float64(hScreenSize)/float64(vScreenSize)))
		}
	}

	// byte 23
	d.FieldU8("gamma", scalar.UintFn(func(s scalar.Uint) (scalar.Uint, error) {
		s.Sym = (float64(s.Actual) / 100) + 1
		return s, nil
	}))

	// byte 24
	d.FieldBool("dpms_standby_supported")
	d.FieldBool("dpms_suspend_supported")
	d.FieldBool("dpms_active_off_supported")
	if inputType == 1 { // digital
		d.FieldU2("display_type", scalar.UintMapDescription{
			0: "RGB 4:4:4",
			1: "RGB 4:4:4 + YCrCb 4:4:4",
			2: "RGB 4:4:4 + YCrCb 4:2:2",
			3: "RGB 4:4:4 + YCrCb 4:4:4 + YCrCb 4:2:2",
		})
	} else { // analog
		d.FieldU2("display_type", scalar.UintMapDescription{
			0: "Monochrome or Grayscale",
			1: "RGB color",
			2: "Non-RGB color",
			3: "undefined",
		})
	}
	d.FieldBool("standard_srgb_color_space")
	if ec.revision == 3 || ec.revision == 4 {
		d.FieldBool("preferred_timing_block_includes_npf_rr")
	} else {
		d.FieldBool("preferred_timing_mode_in_block_1")
	}

	ec.contFreq = d.FieldBool("continuous_frequency_supported")
}

func decodeChromaticityCoordinates(d *decode.D) {
	// byte 25
	ccStart := d.Pos()
	redX0 := d.U2()   // red_x lower 2 bits
	redY0 := d.U2()   // red_y lower 2 bits
	greenX0 := d.U2() // green_x lower 2 bits
	greenY0 := d.U2() // green_y lower 2 bits
	// byte 26
	blueX0 := d.U2()  // blue_x lower 2 bits
	blueY0 := d.U2()  // blue_y lower 2 bits
	whiteX0 := d.U2() // white_x lower 2 bits
	whiteY0 := d.U2() // white_y lower 2 bits
	// byte 27-34
	redX1 := d.U8()   // red_x upper 8 bits
	redY1 := d.U8()   // red_y upper 8 bits
	greenX1 := d.U8() // green_x upper 8 bits
	greenY1 := d.U8() // green_y upper 8 bits
	blueX1 := d.U8()  // blue_x upper 8 bits
	blueY1 := d.U8()  // blue_y upper 8 bits
	whiteX1 := d.U8() // white_x upper 8 bits
	whiteY1 := d.U8() // white_y upper 8 bits
	ccLen := d.Pos() - ccStart

	FieldValueFltAddr(d, "red_x", (float64(redX0)+float64(redX1<<2))/float64(1024), ccStart, ccLen)
	FieldValueFltAddr(d, "red_y", (float64(redY0)+float64(redY1<<2))/float64(1024), ccStart, ccLen)
	FieldValueFltAddr(d, "green_x", (float64(greenX0)+float64(greenX1<<2))/float64(1024), ccStart, ccLen)
	FieldValueFltAddr(d, "green_y", (float64(greenY0)+float64(greenY1<<2))/float64(1024), ccStart, ccLen)
	FieldValueFltAddr(d, "blue_x", (float64(blueX0)+float64(blueX1<<2))/float64(1024), ccStart, ccLen)
	FieldValueFltAddr(d, "blue_y", (float64(blueY0)+float64(blueY1<<2))/float64(1024), ccStart, ccLen)
	FieldValueFltAddr(d, "white_x", (float64(whiteX0)+float64(whiteX1<<2))/float64(1024), ccStart, ccLen)
	FieldValueFltAddr(d, "white_y", (float64(whiteY0)+float64(whiteY1<<2))/float64(1024), ccStart, ccLen)
}

func decodeEstablishedTimings(d *decode.D) {
	// byte 35-37
	e1Start := d.Pos()
	establishedTimingsI := d.U8()
	e2Start := d.Pos()
	establishedTimingsII := d.U8()
	mStart := d.Pos()
	manufacturerTimings := d.U8()
	mapTimings(d, establishedTimingsI, e1Start, establishedTimingsIMapper)
	mapTimings(d, establishedTimingsII, e2Start, establishedTimingsIIMapper)
	mapTimings(d, manufacturerTimings, mStart, manufacturerTimingsMapper)
}

var horizontalAddressablePixelsMapper = scalar.UintFn(func(s scalar.Uint) (scalar.Uint, error) {
	s.Sym = (s.Actual + 31) * 8
	return s, nil
})

func descUintMapper(desc string) scalar.UintFn {
	return scalar.UintFn(func(s scalar.Uint) (scalar.Uint, error) {
		s.Description = desc
		return s, nil
	})
}

func multiUintMapper(m uint64) scalar.UintFn {
	return scalar.UintFn(func(s scalar.Uint) (scalar.Uint, error) {
		s.Sym = s.Actual * m
		return s, nil
	})
}

func addUintMapper(m uint64) scalar.UintFn {
	return scalar.UintFn(func(s scalar.Uint) (scalar.Uint, error) {
		s.Sym = s.Actual + m
		return s, nil
	})
}

var pixelClockMapper = scalar.UintFn(func(s scalar.Uint) (scalar.Uint, error) {
	s.Sym = float64(s.Actual) / float64(100)
	return s, nil
})

var refreshRateMapper = addUintMapper(60)

func decodeStandardTimings(d *decode.D) {
	// byte 38-53
	comboStart := d.Pos()
	timing1to4 := d.U64()
	timing5to8 := d.U64()

	var timingBlock = func(d *decode.D, block uint64, firstBit int64) {
		for i := 3; i >= 0; i-- {
			blockIdx := int64(3 - i)
			fStart := firstBit + (blockIdx * 16)
			sStart := fStart + 8
			first := block >> ((i<<1 + 1) << 3) & 0xf
			second := block >> ((i << 1) << 3) & 0xf

			if first != 1 || second != 1 { // 0x0101 marks an unused standard timing slot
				d.FieldStruct("timing", func(d *decode.D) {
					FieldValueUintAddr(d, "horizontal_adressable_pixels", first, fStart, 8, horizontalAddressablePixelsMapper)
					aspect := second >> 6 & 0x3
					FieldValueUintAddr(d, "aspect_ratio", aspect, sStart, 2, scalar.UintMapSymStr{
						0: "16:10",
						1: "4:3",
						2: "5:4",
						3: "16:9"})
					refresh := second & 0x3f
					FieldValueUintAddr(d, "refresh_rate", refresh, sStart+2, 6, refreshRateMapper)
				})
			}
		}
	}

	timingBlock(d, timing1to4, comboStart)
	timingBlock(d, timing5to8, comboStart+64)
}

var stereoMapper = scalar.UintFn(func(s scalar.Uint) (scalar.Uint, error) {
	test := s.Actual & 0x61
	switch test {
	case 0, 1:
		s.Sym = "normal"
	case 32:
		s.Sym = "field_seq_right"
	case 33:
		s.Sym = "2way_right"
	case 64:
		s.Sym = "field_seq_left"
	case 65:
		s.Sym = "2way_left"
	case 96:
		s.Sym = "4way"
	case 97:
		s.Sym = "side_by_side"
	}
	return s, nil
})

func DetailedDescriptor(d *decode.D, name string, blockNo uint64) {
	blockType := d.PeekUintBits(16)
	var blockName = ""
	if blockType == 0 {
		blockName = fmt.Sprintf("display_descriptor_%d", blockNo-1)
	} else {
		if blockNo > 1 {
			blockName = fmt.Sprintf("%s_%d", name, blockNo)
		} else {
			blockName = name
		}
	}
	d.FieldStruct(blockName, func(d *decode.D) {
		d.FramedFn(18*8, func(d *decode.D) {
			if blockType == 0 { // display descriptor
				d.FieldRawLen("identifer", 3*8, d.AssertBitBuf([]byte{0x00, 0x00, 0x00}))
				tag := d.FieldU8("tag", scalar.UintMapDescription{
					0xff: "Display Product Serial Number",
					0xfe: "Alphanumeric Data String (ASCII)",
					0xfd: "Display Range Limits",
					0xfc: "Display Product Name",
					0xfb: "Color Point Data",
					0xfa: "Standard Timing Identifications",
					0xf9: "Display Color Management (DCM) Data",
					0xf8: "CVT 3 Byte Timing Codes",
					0xf7: "Established Timings III",
					0x10: "Dummy Descriptor",
				}, scalar.UintHex)
				maybeFlags := d.FieldU8("reserved")
				switch tag {
				case 0xfc, 0xfe, 0xff:
					d.FieldStr("value", 13, encoding.Nop, scalar.StrActualTrim("\n "))
				case 0xfd: // monitor range limits
					d.FieldStruct("data", func(d *decode.D) {
						minVertOffset := 0
						if maybeFlags&0x3 == 0x3 {
							minVertOffset = 255
						}
						d.FieldU8("min_vertical_refresh", scalar.UintActualAdd(minVertOffset), descUintMapper("Hz"))

						maxVertOffset := 0
						if maybeFlags&0x2 == 0x2 {
							maxVertOffset = 255
						}
						d.FieldU8("max_vertical_refresh", scalar.UintActualAdd(maxVertOffset), descUintMapper("Hz"))

						minHoriOffset := 0
						if maybeFlags>>2&0x3 == 0x3 {
							minHoriOffset = 255
						}
						d.FieldU8("min_horizontal_refresh", scalar.UintActualAdd(minHoriOffset), descUintMapper("kHz"))

						maxHoriOffset := 0
						if maybeFlags>>2&0x2 == 0x2 {
							maxHoriOffset = 255
						}
						d.FieldU8("max_horizontal_refresh", scalar.UintActualAdd(maxHoriOffset), descUintMapper("kHz"))

						d.FieldU8("max_pixel_clock", multiUintMapper(10), descUintMapper("MHz"))

						vtFlags := d.FieldU8("video_timing_support_flags")
						if vtFlags&0x1 == 0x1 || vtFlags&0x1 == 0x0 {
							d.FieldRawLen("padding", 7*8, d.AssertBitBuf([]byte{0x0a, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}))
						} else {
							// TODO: specify these
							d.FieldU8("video_timing_data1")
							d.FieldU8("video_timing_data2")
							d.FieldU8("video_timing_data3")
							d.FieldU8("video_timing_data4")
							d.FieldU8("video_timing_data5")
							d.FieldU8("video_timing_data6")
							d.FieldU8("video_timing_data7")
						}
					})
				case 0xfa:
					d.FieldRawLen("standard_timing_identifiers", d.BitsLeft())
				case 0xfb:
					d.FieldRawLen("color_point", d.BitsLeft())
				default:
					d.FieldRawLen("manufacturer_tag", d.BitsLeft())
				}
			} else { // detailed timing descriptor
				DetailedTimingDescriptor(d)
			}
		})
	})
}

func DetailedTimingDescriptor(d *decode.D) {
	d.FieldU16("pixel_clock", pixelClockMapper, descUintMapper("MHz"))

	hStart := d.Pos()
	hav0 := d.U8()    // horizontal_addressable_video lower 8 bits
	hblank0 := d.U8() // horizontal_blanking lower 8 bits
	hav1 := d.U4()    // horizontal_addressable_video upper 4 bits
	hblank1 := d.U4() // horizontal_blanking upper 4 bits
	hLen := d.Pos() - hStart
	hblank := hblank0 + (hblank1 << 8)

	FieldValueUintAddr(d, "horizontal_addressable_video", hav0+(hav1<<8), hStart, hLen, descUintMapper("pixels"))
	FieldValueUintAddr(d, "horizontal_blanking", hblank, hStart, hLen, descUintMapper("pixels"))

	vStart := d.Pos()
	vav0 := d.U8()    // vertical_addressable_video lower 8 bits
	vblank0 := d.U8() // vertical_blanking lower 8 bits
	vav1 := d.U4()    // vertical_addressable_video upper 4 bits
	vblank1 := d.U4() // vertical_blanking upper 4 bits
	vLen := d.Pos() - vStart
	vblank := vblank0 + (vblank1 << 8)

	FieldValueUintAddr(d, "vertical_addressable_video", vav0+(vav1<<8), vStart, vLen, descUintMapper("lines"))
	FieldValueUintAddr(d, "vertical_blanking", vblank, vStart, vLen, descUintMapper("lines"))

	pStart := d.Pos()
	hfp0 := d.U8()  // horizontal_front_porch lower 8 bits
	hspw0 := d.U8() // horizontal_sync_pulse_width lower 8 bits
	vfp0 := d.U4()  // vertical_front_porch lower 4 bits
	vspw0 := d.U4() // vertical_sync_pulse_width lower 4 bits
	hfp1 := d.U2()  // horizontal_front_porch upper 2 bits
	hspw1 := d.U2() // horizontal_sync_pulse_width upper 2 bits
	vfp1 := d.U2()  // vertical_front_porch upper 2 bits
	vspw1 := d.U2() // vertical_sync_pulse_width upper 2 bits
	pLen := d.Pos() - pStart

	hfp := hfp0 + (hfp1 << 8)
	hspw := hspw0 + (hspw1 << 8)
	vfp := vfp0 + (vfp1 << 8)
	vspw := vspw0 + (vspw1 << 8)

	FieldValueUintAddr(d, "horizontal_front_porch", hfp, pStart, pLen, descUintMapper("pixels"))
	FieldValueUintAddr(d, "horizontal_sync_pulse_width", hspw, pStart, pLen, descUintMapper("pixels"))
	FieldValueUintAddr(d, "horizontal_back_porch", hblank-hfp-hspw, pStart, pLen, descUintMapper("pixels"))
	FieldValueUintAddr(d, "vertical_front_porch", vfp, pStart, pLen, descUintMapper("lines"))
	FieldValueUintAddr(d, "vertical_sync_pulse_width", vspw, pStart, pLen, descUintMapper("lines"))
	FieldValueUintAddr(d, "vertical_back_porch", vblank-vfp-vspw, pStart, pLen, descUintMapper("lines"))

	iStart := d.Pos()
	havis0 := d.U8() // horizontal_addressable_video_image_size lower 8 bits
	vavis0 := d.U8() // vertical_addressable_video_image_size lower 8 bits
	havis1 := d.U4() // horizontal_addressable_video_image_size upper 4 bits
	vavis1 := d.U4() // vertical_addressable_video_image_size upper 4 bits
	iLen := d.Pos() - iStart

	FieldValueUintAddr(d, "horizontal_addressable_video_image_size", havis0+(havis1<<8), iStart, iLen, descUintMapper("mm"))
	FieldValueUintAddr(d, "vertical_addressable_video_image_size", vavis0+(vavis1<<8), iStart, iLen, descUintMapper("mm"))

	d.FieldU8("horizontal_border_left_right", descUintMapper("mm"))
	d.FieldU8("vertical_border_left_right", descUintMapper("mm"))

	d.FieldU1("signal_interface_type", scalar.UintMapSymStr{0: "non-interlaced", 1: "interlaced"})
	d.FieldU7("stereo_viewing_support", stereoMapper)
	d.SeekRel(-5)
	sStart := d.Pos()
	sync := d.U4()
	sLen := d.Pos() - sStart
	d.SeekRel(1)

	firstTwo := sync & 0xc
	FieldValueUintAddr(d, "sync_signal", firstTwo, sStart, sLen, scalar.UintMapSymStr{
		0b0000: "analog_composite",
		0b0100: "bipolar_analog_composite",
		0b1000: "digital_composite",
		0b1100: "digital_separate"})
	third := sync & 0x2
	fourth := sync & 0x1
	if firstTwo == 0b1100 {
		FieldValueUintAddr(d, "vsync_polarity", third, sStart, sLen, scalar.UintMapSymStr{0: "negative", 1: "positive"})
	} else {
		FieldValueUintAddr(d, "with_serrations", third, sStart, sLen, scalar.UintMapSymBool{0: false, 1: true})
	}
	if firstTwo == 0b1100 || firstTwo == 0b1000 {
		FieldValueUintAddr(d, "hsync_polarity", fourth, sStart, sLen, scalar.UintMapSymStr{0: "negative", 1: "positive"})
	} else {
		FieldValueUintAddr(d, "sync_on", fourth, sStart, sLen, scalar.UintMapSymStr{0: "green_only", 1: "rgb"})
	}
}

func decodeEDID(d *decode.D) any {
	var ec edidContext

	d.Endian = decode.LittleEndian

	d.FramedFn(20*8, func(d *decode.D) {
		d.FieldStruct("header", func(d *decode.D) { decodeFileHeader(d, &ec) })
	})

	d.FramedFn(5*8, func(d *decode.D) {
		d.FieldStruct("basic_display_parameters", func(d *decode.D) { decodeBasicDisplayParameters(d, &ec) })
	})

	d.FramedFn(10*8, func(d *decode.D) {
		d.FieldStruct("chromaticity_coordinates", decodeChromaticityCoordinates)
	})

	d.FramedFn(3*8, func(d *decode.D) {
		d.FieldArray("established_timings", decodeEstablishedTimings)
	})

	d.FramedFn(16*8, func(d *decode.D) {
		d.FieldArray("standard_timings", decodeStandardTimings)
	})

	d.FieldStruct("detailed_timings", func(d *decode.D) {
		DetailedDescriptor(d, "preferred_timing_mode", 1)
		DetailedDescriptor(d, "detailed_timing_descriptor", 2)
		DetailedDescriptor(d, "detailed_timing_descriptor", 3)
		DetailedDescriptor(d, "detailed_timing_descriptor", 4)
	})

	ext := d.FieldU8("extension_count")

	// sum of all 128 bytes should be 0
	sum := CalcSum(d.BytesRange(0, 127))
	// so checksum is 0 minus sum of first 127 bytes
	d.FieldU8("checksum", d.UintValidate(uint64(0-sum)), scalar.UintHex)

	if ext > 0 {
		d.FieldArray("extensions", func(d *decode.D) {
			for range ext {
				d.FramedFn(128*8, func(d *decode.D) {
					dv, _, _ := d.TryFieldFormat("extension", &edidExtensionGroup, nil)
					if dv == nil {
						d.FieldRawLen("unknown_extension", 128*8)
					}
				})
				// mfo, ok := v.(format.EDID_Extension)
				// if !ok {
				// 	panic(fmt.Sprintf("expected EDID_Extension got %#+v", v))
				// }

				//     d.FieldU8("tag", scalar.UintMapDescription{
				// 	0x02: "CEA-861 Series Timing Extension",
				// 	0x10: "Video Timing Block Extension (VTB-EXT)",
				// 	0x20: "EDID 2.0 Extension",
				// 	0x40: "Display Information Extension (DI-EXT)",
				// 	0x50: "Localized String Extension (LS-EXT)",
				// 	0x60: "Microdisplay Interface Extension (MI-EXT)",
				// 	0x70: "DisplayID Extension",
				// 	0xF0: "Block Map",
				// 	0xFF: "Manufacturer Defined Extension",
				// }, scalar.UintHex)
				// d.FieldRawLen("data", 126*8)
				//
				// sum := CalcSum(d.BytesRange(128*8*int64(i+1), 127))
				// d.FieldU8("checksum", d.UintValidate(uint64(0-sum)), scalar.UintHex)
			}
		})
	}

	return nil
}
