//go:build windows

package mcp

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// Windows DPAPI (requirement 8 / SPEC §5.2): the grant file is encrypted with
// the *current user's* credentials, so another account on the same machine
// cannot read the endpoint or its auth. Implemented with crypt32 directly —
// adding a cgo or third-party dependency just for two syscalls is not worth it.

var (
	crypt32           = syscall.NewLazyDLL("crypt32.dll")
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procProtectData   = crypt32.NewProc("CryptProtectData")
	procUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree     = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	size uint32
	data *byte
}

const (
	uiForbidden   = 0x1 // CRYPTPROTECT_UI_FORBIDDEN
	extraEntropy  = "local.es.studio/mcp-grant/v1"
	sealAlgorithm = "dpapi"
)

func seal(plain []byte) ([]byte, string, error) {
	entropy := []byte(extraEntropy)
	in := dataBlob{size: uint32(len(plain)), data: &plain[0]}
	ent := dataBlob{size: uint32(len(entropy)), data: &entropy[0]}
	var out dataBlob
	ok, _, err := procProtectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, uintptr(unsafe.Pointer(&ent)), 0, 0, uiForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		return nil, "", fmt.Errorf("dpapi protect failed: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.data)))
	// Copy before releasing the LocalAlloc buffer.
	sealed := make([]byte, out.size)
	copy(sealed, unsafe.Slice(out.data, out.size))
	return sealed, sealAlgorithm, nil
}

func unseal(sealed []byte, algorithm string) ([]byte, error) {
	if algorithm != sealAlgorithm {
		return nil, fmt.Errorf("grant was sealed with %q, this build can only open dpapi grants", algorithm)
	}
	if len(sealed) == 0 {
		return nil, errors.New("empty sealed grant")
	}
	entropy := []byte(extraEntropy)
	in := dataBlob{size: uint32(len(sealed)), data: &sealed[0]}
	ent := dataBlob{size: uint32(len(entropy)), data: &entropy[0]}
	var out dataBlob
	ok, _, err := procUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, uintptr(unsafe.Pointer(&ent)), 0, 0, uiForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		return nil, fmt.Errorf("dpapi unprotect failed (grant belongs to another user?): %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.data)))
	plain := make([]byte, out.size)
	copy(plain, unsafe.Slice(out.data, out.size))
	return plain, nil
}
