package edid

func CalcSum(bytes []byte) uint8 {
	var checksum uint8 = 0
	for i := 0; i < len(bytes); i++ {
		checksum += bytes[i]
	}
	return checksum
}
