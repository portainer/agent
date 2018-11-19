//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package pcidb

import (
	"fmt"
	"os"
	"strconv"
)

var (
	cacheOnlyTrue = true
)

// ProgrammingInterface is the PCI programming interface for a class of PCI
// devices
type ProgrammingInterface struct {
	ID   string // hex-encoded PCI_ID of the programming interface
	Name string // common string name for the programming interface
}

// Subclass is a subdivision of a PCI class
type Subclass struct {
	ID                    string                  // hex-encoded PCI_ID for the device subclass
	Name                  string                  // common string name for the subclass
	ProgrammingInterfaces []*ProgrammingInterface // any programming interfaces this subclass might have
}

// Class is the PCI class
type Class struct {
	ID         string      // hex-encoded PCI_ID for the device class
	Name       string      // common string name for the class
	Subclasses []*Subclass // any subclasses belonging to this class
}

// Product provides information about a PCI device model
// NOTE(jaypipes): In the hardware world, the PCI "device_id" is the identifier
// for the product/model
type Product struct {
	VendorID   string     // vendor ID for the product
	ID         string     // hex-encoded PCI_ID for the product/model
	Name       string     // common string name of the vendor
	Subsystems []*Product // "subdevices" or "subsystems" for the product
}

// Vendor provides information about a device vendor
type Vendor struct {
	ID       string     // hex-encoded PCI_ID for the vendor
	Name     string     // common string name of the vendor
	Products []*Product // all top-level devices for the vendor
}

type PCIDB struct {
	// hash of class ID -> class information
	Classes map[string]*Class
	// hash of vendor ID -> vendor information
	Vendors map[string]*Vendor
	// hash of vendor ID + product/device ID -> product information
	Products map[string]*Product
}

// WithOption is used to represent optionally-configured settings
type WithOption struct {
	// Chroot is the directory that pcidb uses when attempting to discover
	// pciids DB files
	Chroot *string
	// CacheOnly is mostly just useful for testing. It essentially disables
	// looking for any non ~/.cache/pci.ids filepaths (which is useful when we
	// want to test the fetch-from-network code paths
	CacheOnly *bool
}

func WithChroot(dir string) *WithOption {
	return &WithOption{Chroot: &dir}
}

func WithCacheOnly() *WithOption {
	return &WithOption{CacheOnly: &cacheOnlyTrue}
}

func mergeOptions(opts ...*WithOption) *WithOption {
	// Grab options from the environs by default
	defaultChroot := "/"
	if val, exists := os.LookupEnv("PCIDB_CHROOT"); exists {
		defaultChroot = val
	}
	defaultCacheOnly := false
	if val, exists := os.LookupEnv("PCIDB_CACHE_ONLY"); exists {
		if parsed, err := strconv.ParseBool(val); err != nil {
			fmt.Fprintf(
				os.Stderr,
				"Failed parsing a bool from PCIDB_CACHE_ONLY "+
					"environ value of %s",
				val,
			)
		} else if parsed {
			defaultCacheOnly = parsed
		}
	}
	merged := &WithOption{}
	for _, opt := range opts {
		if opt.Chroot != nil {
			merged.Chroot = opt.Chroot
		}
		if opt.CacheOnly != nil {
			merged.CacheOnly = opt.CacheOnly
		}
	}
	// Set the default value if missing from merged
	if merged.Chroot == nil {
		merged.Chroot = &defaultChroot
	}
	if merged.CacheOnly == nil {
		merged.CacheOnly = &defaultCacheOnly
	}
	return merged
}

// New returns a pointer to a PCIDB struct which contains information you can
// use to query PCI vendor, product and class information. It accepts zero or
// more pointers to WithOption structs. If you want to modify the behaviour of
// pcidb, use one of the option modifiers when calling New. For example, to
// change the root directory that pcidb uses when discovering pciids DB files,
// call New(WithChroot("/my/root/override"))
func New(opts ...*WithOption) (*PCIDB, error) {
	ctx := contextFromOptions(mergeOptions(opts...))
	db := &PCIDB{}
	err := db.load(ctx)
	return db, err
}
