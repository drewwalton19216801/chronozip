package chronozip

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// HISTORY_SIZE determines the lookback window size. Max offset is HISTORY_SIZE.
	HISTORY_SIZE = 32768
	// MIN_RUN_LENGTH is the minimum length for RLE encoding.
	// Cost: Marker(1) + Char(1) + Length(2) = 4 bytes. Must replace > 4 bytes.
	MIN_RUN_LENGTH = 5
	// MIN_MATCH_LENGTH is the minimum length for back-reference encoding.
	// Cost: Marker(1) + Offset(2) + Length(2) = 5 bytes. Must replace > 5 bytes.
	MIN_MATCH_LENGTH = 6

	// Markers
	rleMarker      byte = 0xFF
	backRefMarker  byte = 0xFE
	literalMarker  byte = 0xFD
	maxLiteralByte byte = 0xFC // Highest value for a direct literal
)

var (
	errInvalidOffset = errors.New("chronozip: invalid back-reference offset or length")
	errCorruptStream = errors.New("chronozip: corrupt input stream")
)

// --- Compression ---

type Compressor struct {
	writer        *bufio.Writer
	historyBuffer []byte
}

func NewCompressor(w io.Writer) *Compressor {
	return &Compressor{
		writer:        bufio.NewWriter(w),
		historyBuffer: make([]byte, 0, HISTORY_SIZE*2), // Pre-allocate some capacity
	}
}

func (c *Compressor) Flush() error {
	return c.writer.Flush()
}

// appendToHistory adds data to history and trims if necessary
func (c *Compressor) appendToHistory(data []byte) {
	c.historyBuffer = append(c.historyBuffer, data...)
	if len(c.historyBuffer) > HISTORY_SIZE {
		excess := len(c.historyBuffer) - HISTORY_SIZE
		// More efficient slice trimming without allocation
		c.historyBuffer = c.historyBuffer[excess:]
		// Ensure capacity doesn't grow indefinitely if we only trim small amounts often
		// This check is optional but can help manage memory if HISTORY_SIZE is huge
		// and trimming happens very frequently. For 32k it might be okay.
		if cap(c.historyBuffer) > HISTORY_SIZE*2 && len(c.historyBuffer) < HISTORY_SIZE+(HISTORY_SIZE/4) {
			newSlice := make([]byte, len(c.historyBuffer), HISTORY_SIZE*2)
			copy(newSlice, c.historyBuffer)
			c.historyBuffer = newSlice
		}
	}
}

// writeUint16 writes a uint16 in BigEndian format
func (c *Compressor) writeUint16(val uint16) error {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], val)
	_, err := c.writer.Write(buf[:])
	return err
}

// writeByte writes a single byte
func (c *Compressor) writeByte(b byte) error {
	return c.writer.WriteByte(b)
}

// findLongestRun finds the length of the RLE sequence at the start of data
func findLongestRun(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	char := data[0]
	length := 1
	for length < len(data) && length < 65535 && data[length] == char { // uint16 max length
		length++
	}
	return length
}

// findBestMatch searches history for the longest match with the start of data
func (c *Compressor) findBestMatch(data []byte) (offset, length uint16) {
	if len(data) == 0 || len(c.historyBuffer) == 0 {
		return 0, 0
	}

	bestLen := uint16(0)
	bestOffset := uint16(0)
	maxPossibleLen := uint16(len(data))
	if maxPossibleLen > 65535 {
		maxPossibleLen = 65535 // Clamp to uint16 max
	}

	// Iterate backwards through history buffer start positions
	histLen := len(c.historyBuffer)
	for i := 0; i < histLen; i++ {
		// Check if potential match starting at historyBuffer[i] can be long enough
		// and if the first bytes match
		if histLen-i < int(bestLen) || c.historyBuffer[i] != data[0] {
			continue
		}

		// Calculate current match length
		currentLen := uint16(0)
		maxMatchCheck := maxPossibleLen
		if histLen-i < int(maxMatchCheck) {
			maxMatchCheck = uint16(histLen - i)
		}

		for currentLen < maxMatchCheck && c.historyBuffer[i+int(currentLen)] == data[currentLen] {
			currentLen++
		}

		// If this match is better, store it
		if currentLen > bestLen {
			bestLen = currentLen
			bestOffset = uint16(histLen - i) // Offset is distance from end
		}

		// Small optimization: If we found the longest possible match, stop searching
		if bestLen == maxPossibleLen {
			break
		}
	}

	// Only return matches that meet the minimum length requirement
	if bestLen >= MIN_MATCH_LENGTH {
		return bestOffset, bestLen
	}
	return 0, 0
}

// Compress reads from r and writes compressed data to the Compressor's writer.
func (c *Compressor) Compress(r io.Reader) error {
	// Use a buffered reader for potentially better performance
	bufReader := bufio.NewReader(r)
	inputBuffer := make([]byte, HISTORY_SIZE*2) // Read ahead buffer
	bytesRead := 0
	eof := false

	for !eof {
		// Fill the buffer as much as possible
		n, err := bufReader.Read(inputBuffer[bytesRead:])
		if err != nil && err != io.EOF {
			return fmt.Errorf("chronozip: error reading input: %w", err)
		}
		if err == io.EOF {
			eof = true
		}
		bytesRead += n

		if bytesRead == 0 && eof {
			break // Nothing more to read or process
		}

		processed := 0
		for processed < bytesRead {
			remainingData := inputBuffer[processed:bytesRead]

			// Ensure we have enough lookahead data if not at EOF
			if !eof && len(remainingData) < MIN_MATCH_LENGTH && bytesRead < len(inputBuffer) {
				// Not enough data to make good decisions, break to read more
				break
			}

			// 1. Try RLE
			runLength := findLongestRun(remainingData)
			if runLength >= MIN_RUN_LENGTH {
				char := remainingData[0]
				if err := c.writeByte(rleMarker); err != nil {
					return err
				}
				if err := c.writeByte(char); err != nil {
					return err
				}
				if err := c.writeUint16(uint16(runLength)); err != nil {
					return err
				}

				c.appendToHistory(remainingData[:runLength])
				processed += runLength
				continue
			}

			// 2. Try Back-Reference
			offset, length := c.findBestMatch(remainingData)
			if length >= MIN_MATCH_LENGTH { // Check length again (findBestMatch ensures >= MIN_MATCH_LENGTH)
				if err := c.writeByte(backRefMarker); err != nil {
					return err
				}
				if err := c.writeUint16(offset); err != nil {
					return err
				}
				if err := c.writeUint16(length); err != nil {
					return err
				}

				c.appendToHistory(remainingData[:length])
				processed += int(length)
				continue
			}

			// 3. Output Literal
			byteToWrite := remainingData[0]
			if byteToWrite >= literalMarker { // Check if it's one of the special markers
				if err := c.writeByte(literalMarker); err != nil {
					return err
				}
				if err := c.writeByte(byteToWrite); err != nil {
					return err
				}
			} else {
				if err := c.writeByte(byteToWrite); err != nil {
					return err
				}
			}
			c.appendToHistory([]byte{byteToWrite})
			processed += 1
		}

		// Shift unprocessed data to the beginning of the buffer
		if processed > 0 {
			copy(inputBuffer[0:], inputBuffer[processed:bytesRead])
			bytesRead -= processed
		}

		// If buffer is full and we couldn't process anything, something is wrong (shouldn't happen with literal fallback)
		if bytesRead == len(inputBuffer) && processed == 0 {
			// As a fallback, write the first byte as literal to ensure progress
			byteToWrite := inputBuffer[0]
			if byteToWrite >= literalMarker {
				if err := c.writeByte(literalMarker); err != nil {
					return err
				}
				if err := c.writeByte(byteToWrite); err != nil {
					return err
				}
			} else {
				if err := c.writeByte(byteToWrite); err != nil {
					return err
				}
			}
			c.appendToHistory([]byte{byteToWrite})
			copy(inputBuffer[0:], inputBuffer[1:bytesRead])
			bytesRead--
		}
	}

	return c.Flush() // Ensure everything is written
}

// --- Decompression ---

type Decompressor struct {
	reader        *bufio.Reader
	historyBuffer []byte
}

func NewDecompressor(r io.Reader) *Decompressor {
	return &Decompressor{
		reader:        bufio.NewReader(r),
		historyBuffer: make([]byte, 0, HISTORY_SIZE*2),
	}
}

// appendToHistory adds data to history and trims if necessary (same as compressor)
func (d *Decompressor) appendToHistory(data []byte) {
	d.historyBuffer = append(d.historyBuffer, data...)
	if len(d.historyBuffer) > HISTORY_SIZE {
		excess := len(d.historyBuffer) - HISTORY_SIZE
		// More efficient slice trimming without allocation
		d.historyBuffer = d.historyBuffer[excess:]
		// Ensure capacity doesn't grow indefinitely if we only trim small amounts often
		// This check is optional but can help manage memory if HISTORY_SIZE is huge
		// and trimming happens very frequently. For 32k it might be okay.
		if cap(d.historyBuffer) > HISTORY_SIZE*2 && len(d.historyBuffer) < HISTORY_SIZE+(HISTORY_SIZE/4) {
			newSlice := make([]byte, len(d.historyBuffer), HISTORY_SIZE*2)
			copy(newSlice, d.historyBuffer)
			d.historyBuffer = newSlice
		}
	}
}

// readUint16 reads a uint16 in BigEndian format
func (d *Decompressor) readUint16() (uint16, error) {
	var buf [2]byte
	_, err := io.ReadFull(d.reader, buf[:])
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("%w: reading uint16", errCorruptStream)
		}
		return 0, err
	}
	return binary.BigEndian.Uint16(buf[:]), nil
}

// readByte reads a single byte
func (d *Decompressor) readByte() (byte, error) {
	b, err := d.reader.ReadByte()
	if errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("%w: reading byte", errCorruptStream) // Unexpected EOF inside a sequence
	}
	return b, err
}

// Decompress reads from the Decompressor's reader and writes decompressed data to w.
func (d *Decompressor) Decompress(w io.Writer) error {
	bufWriter := bufio.NewWriter(w)
	defer bufWriter.Flush() // Ensure flush happens even on errors

	for {
		marker, err := d.reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break // Clean EOF, end of stream
			}
			return fmt.Errorf("chronozip: error reading marker: %w", err)
		}

		switch marker {
		case rleMarker:
			char, err := d.readByte()
			if err != nil {
				return err
			}
			count, err := d.readUint16()
			if err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("%w: RLE count cannot be zero", errCorruptStream)
			}

			run := bytes.Repeat([]byte{char}, int(count))
			if _, err := bufWriter.Write(run); err != nil {
				return fmt.Errorf("chronozip: error writing RLE data: %w", err)
			}
			d.appendToHistory(run)

		case backRefMarker:
			offset, err := d.readUint16()
			if err != nil {
				return err
			}
			length, err := d.readUint16()
			if err != nil {
				return err
			}
			if offset == 0 || length == 0 {
				return fmt.Errorf("%w: back-reference offset/length cannot be zero", errCorruptStream)
			}

			histLen := len(d.historyBuffer)
			start := histLen - int(offset)
			end := start + int(length)

			if start < 0 || end > histLen {
				return fmt.Errorf("%w: offset %d length %d exceeds history size %d", errInvalidOffset, offset, length, histLen)
			}

			match := d.historyBuffer[start:end]
			// Need to copy the match *before* appending, in case the match overlaps with the area being added
			matchCopy := make([]byte, len(match))
			copy(matchCopy, match)

			if _, err := bufWriter.Write(matchCopy); err != nil {
				return fmt.Errorf("chronozip: error writing back-ref data: %w", err)
			}
			d.appendToHistory(matchCopy) // Append the copied data

		case literalMarker:
			literalByte, err := d.readByte()
			if err != nil {
				return err
			}
			if err := bufWriter.WriteByte(literalByte); err != nil {
				return fmt.Errorf("chronozip: error writing escaped literal: %w", err)
			}
			d.appendToHistory([]byte{literalByte})

		default: // Direct literal byte
			if err := bufWriter.WriteByte(marker); err != nil {
				return fmt.Errorf("chronozip: error writing literal: %w", err)
			}
			d.appendToHistory([]byte{marker})
		}
	}

	// Flush remaining data in the buffered writer
	return bufWriter.Flush()
}

// Helper functions for easy use

// CompressData compresses a byte slice
func CompressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	compressor := NewCompressor(&buf)
	err := compressor.Compress(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	// No need to flush here, Compress does it
	return buf.Bytes(), nil
}

// DecompressData decompresses a byte slice
func DecompressData(compressedData []byte) ([]byte, error) {
	var buf bytes.Buffer
	decompressor := NewDecompressor(bytes.NewReader(compressedData))
	err := decompressor.Decompress(&buf)
	if err != nil {
		// Distinguish between clean EOF and actual errors during decompression
		if errors.Is(err, io.EOF) {
			// This might be okay if the input was truncated but some output was generated
			// However, usually Decompress handles EOF internally. If it bubbles up,
			// it might indicate corruption or unexpected end. Let's treat it as an error.
			return buf.Bytes(), fmt.Errorf("%w: unexpected EOF during decompression", errCorruptStream)
		}
		return buf.Bytes(), err // Return partially decompressed data and the error
	}
	// No need to flush, Decompress does it
	return buf.Bytes(), nil
}
