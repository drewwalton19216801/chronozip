package chronozip

import (
	"bytes"
	"math/rand"
	"testing"
)

// BenchmarkCompressRepetitive benchmarks compression of highly repetitive data.
func BenchmarkCompressRepetitive(b *testing.B) {
	data := bytes.Repeat([]byte{'A'}, 1024*10) // 10KB of 'A's
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compressed, err := CompressData(data)
		if err != nil {
			b.Fatalf("CompressData failed: %v", err)
		}
		if compressed == nil {
			b.Fatal("CompressData returned nil")
		}
	}
}

// BenchmarkDecompressRepetitive benchmarks decompression of highly repetitive data.
func BenchmarkDecompressRepetitive(b *testing.B) {
	originalData := bytes.Repeat([]byte{'A'}, 1024*10) // 10KB of 'A's
	compressedData, err := CompressData(originalData)
	if err != nil {
		b.Fatalf("Initial CompressData failed: %v", err)
	}
	if compressedData == nil {
		b.Fatal("Initial CompressData returned nil")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decompressed, err := DecompressData(compressedData)
		if err != nil {
			b.Fatalf("DecompressData failed: %v", err)
		}
		if !bytes.Equal(originalData, decompressed) {
			b.Fatal("Decompressed data does not match original")
		}
	}
}

// Helper function to generate random data
func generateRandomData(size int) []byte {
	data := make([]byte, size)
	_, err := rand.Read(data)
	if err != nil {
		// This should ideally not happen in a test environment
		// but handle it just in case.
		panic("Failed to generate random data: " + err.Error())
	}
	return data
}

// BenchmarkCompressRandom benchmarks compression of random data.
func BenchmarkCompressRandom(b *testing.B) {
	data := generateRandomData(1024 * 10) // 10KB of random data
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compressed, err := CompressData(data)
		if err != nil {
			b.Fatalf("CompressData failed: %v", err)
		}
		if compressed == nil {
			b.Fatal("CompressData returned nil")
		}
	}
}

// BenchmarkDecompressRandom benchmarks decompression of random data.
func BenchmarkDecompressRandom(b *testing.B) {
	originalData := generateRandomData(1024 * 10) // 10KB of random data
	compressedData, err := CompressData(originalData)
	if err != nil {
		b.Fatalf("Initial CompressData failed: %v", err)
	}
	if compressedData == nil {
		b.Fatal("Initial CompressData returned nil")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decompressed, err := DecompressData(compressedData)
		if err != nil {
			b.Fatalf("DecompressData failed: %v", err)
		}
		if !bytes.Equal(originalData, decompressed) {
			b.Fatal("Decompressed data does not match original")
		}
	}
}

const sampleText = `
This is a sample text for benchmarking the chronozip library.
It contains a mix of common English words, punctuation, and newlines.
The purpose is to simulate real-world text data compression and decompression.
We hope this provides a reasonable estimate of performance on typical text files.
Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.
Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat.
Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur.
Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.
Repeating some phrases for good measure:
chronozip benchmark, chronozip benchmark, chronozip benchmark.
Numbers: 1234567890 and symbols: !@#$%^&*()_+{}:"<>?
End of sample text.
`

// BenchmarkCompressText benchmarks compression of sample text data.
func BenchmarkCompressText(b *testing.B) {
	data := []byte(sampleText)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compressed, err := CompressData(data)
		if err != nil {
			b.Fatalf("CompressData failed: %v", err)
		}
		if compressed == nil {
			b.Fatal("CompressData returned nil")
		}
	}
}

// BenchmarkDecompressText benchmarks decompression of sample text data.
func BenchmarkDecompressText(b *testing.B) {
	originalData := []byte(sampleText)
	compressedData, err := CompressData(originalData)
	if err != nil {
		b.Fatalf("Initial CompressData failed: %v", err)
	}
	if compressedData == nil {
		b.Fatal("Initial CompressData returned nil")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decompressed, err := DecompressData(compressedData)
		if err != nil {
			b.Fatalf("DecompressData failed: %v", err)
		}
		if !bytes.Equal(originalData, decompressed) {
			b.Fatal("Decompressed data does not match original")
		}
	}
}
