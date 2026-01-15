package driver

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

func SplitBlock(data []byte, BlockSize int) [][]byte {
	// 定义拆分后的切片集合
	var smallSlices [][]uint8
	var bigSlice = data
	// 计算每个小切片的长度
	chunkSize := BlockSize
	// 循环拆分大切片
	for i := 0; i < len(bigSlice); i += chunkSize {
		end := i + chunkSize
		// 如果最后一个切片长度不足chunkSize，则end设置为实际长度
		if end > len(bigSlice) {
			end = len(bigSlice)
		}
		// 将大切片中的一部分作为小切片添加到集合中
		smallSlices = append(smallSlices, bigSlice[i:end])
	}
	return smallSlices
}

// HexToASCII 将带有空格的十六进制字符串转换为 ASCII 字符串
func HexToASCII(hexStr string) (string, error) {
	// 移除所有空格
	hexStr = strings.ReplaceAll(hexStr, " ", "")

	// 解码十六进制字符串
	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return "", err
	}
	fmt.Println(string(data))
	// 转换为 ASCII 字符串
	return string(data), nil
}

func IntToBig(num uint32) []uint8 {

	// 创建一个足够大的字节切片来存储uint32类型的数据
	buf := make([]byte, 4)
	//var buf []uint8

	// 使用binary.BigEndian.PutUint32将uint32类型的数据写入切片中
	binary.BigEndian.PutUint32(buf, num)

	// 输出结果
	//fmt.Printf("%d\n", buf) // 使用% x格式化输出，以十六进制形式显示每个字节
	return buf
}

func BigToInt(buf []uint8) uint32 {
	if len(buf) < 4 {
		padded := make([]byte, 4)
		copy(padded[4-len(buf):], buf)
		buf = padded
	}
	return binary.BigEndian.Uint32(buf)
}

func HexStringToByteSlice(hexStr string) ([]byte, error) {
	// 检查字符串长度是否为32
	if len(hexStr) != 32 {
		return nil, fmt.Errorf("input string must be 32 characters long")
	}

	result := make([]byte, 16)

	for i := 0; i < 16; i++ {
		// 每两个字符组成一个字节
		byteStr := hexStr[i*2 : i*2+2]
		val, err := strconv.ParseUint(byteStr, 16, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid hex string at position %d: %v", i, err)
		}
		result[i] = byte(val)
	}

	return result, nil
}

/*
- - -—- - -
--- --- ---
--- --- ---
--- --- - -
- - -—- - -
--- - - ---
*/
