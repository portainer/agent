//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package pcidb

type PCIProgrammingInterface struct {
	// Id is DEPRECATED in 0.2 and will be removed in the 1.0 release. Please
	// use the equivalent ID field.
	Id   string
	ID   string // hex-encoded PCI_ID of the programming interface
	Name string // common string name for the programming interface
}

type PCISubclass struct {
	// Id is DEPRECATED in 0.2 and will be removed in the 1.0 release. Please
	// use the equivalent ID field.
	Id                    string
	ID                    string                     // hex-encoded PCI_ID for the device subclass
	Name                  string                     // common string name for the subclass
	ProgrammingInterfaces []*PCIProgrammingInterface // any programming interfaces this subclass might have
}

type PCIClass struct {
	// Id is DEPRECATED in 0.2 and will be removed in the 1.0 release. Please
	// use the equivalent ID field.
	Id         string
	ID         string         // hex-encoded PCI_ID for the device class
	Name       string         // common string name for the class
	Subclasses []*PCISubclass // any subclasses belonging to this class
}

// NOTE(jaypipes): In the hardware world, the PCI "device_id" is the identifier
// for the product/model
type PCIProduct struct {
	// VendorId is DEPRECATED in 0.2 and will be removed in the 1.0 release. Please
	// use the equivalent VendorID field.
	VendorId string
	VendorID string // vendor ID for the product
	// Id is DEPRECATED in 0.2 and will be removed in the 1.0 release. Please
	// use the equivalent ID field.
	Id         string
	ID         string        // hex-encoded PCI_ID for the product/model
	Name       string        // common string name of the vendor
	Subsystems []*PCIProduct // "subdevices" or "subsystems" for the product
}

type PCIVendor struct {
	// Id is DEPRECATED in 0.2 and will be removed in the 1.0 release. Please
	// use the equivalent ID field.
	Id       string
	ID       string        // hex-encoded PCI_ID for the vendor
	Name     string        // common string name of the vendor
	Products []*PCIProduct // all top-level devices for the vendor
}

type PCIDB struct {
	// hash of class ID -> class dbrmation
	Classes map[string]*PCIClass
	// hash of vendor ID -> vendor dbrmation
	Vendors map[string]*PCIVendor
	// hash of vendor ID + product/device ID -> product dbrmation
	Products map[string]*PCIProduct
}

func New() (*PCIDB, error) {
	db := &PCIDB{}
	err := db.load()
	return db, err
}
