// Copyright © 2018 Banzai Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloudinfo

import (
	"context"
)

const (
	// Memory represents the memory attribute for the product info
	Memory = "memory"

	// Cpu represents the cpu attribute for the product info
	Cpu = "cpu"
)

// CloudInfo is the main entry point for retrieving vm type characteristics and pricing information on different cloud providers
type CloudInfo interface {
	// GetProviders returns the supported providers
	GetProviders() []ProviderDescriber

	// GetProvider retrieves information about the provider
	GetProvider(provider string) (ProviderDescriber, error)

	// Start starts the product information retrieval in a new goroutine
	Start(ctx context.Context)

	// Initialize is called once per product info renewals so it can be used to download a large price descriptor
	Initialize(provider string) (map[string]map[string]Price, error)

	// GetAttributes returns the supported attribute names
	GetAttributes() []string

	// GetAttrValues returns a slice with the possible values for a given attribute on a specific provider
	GetAttrValues(provider string, attribute string) ([]float64, error)

	// GetZones returns all the availability zones for a region
	GetZones(provider string, region string) ([]string, error)

	// GetRegions returns all the regions for a cloud provider
	GetRegions(provider string) (map[string]string, error)

	// HasShortLivedPriceInfo signals if a product info provider has frequently changing price info
	HasShortLivedPriceInfo(provider string) bool

	// GetPrice returns the on demand price and the zone averaged computed spot price for a given instance type in a given region
	GetPrice(provider string, region string, instanceType string, zones []string) (float64, float64, error)

	// GetInfoer gets the cloud provider specific Infoer implementation (discriminator for cloud providers)
	GetInfoer(provider string) (CloudInfoer, error)
}

// AttrValue represents an attribute value
type AttrValue struct {
	StrValue string
	Value    float64
}

// AttrValues a slice of AttrValues
type AttrValues []AttrValue

var (
	// telescope supported network performance of vm-s

	// NetwLow the low network performance category
	NetwLow = "low"
	// NtwMedium the medium network performance category
	NtwMedium = "medium"
	// NtwHight the high network performance category
	NtwHight = "high"
	// NtwExtra the highest network performance category
	NtwExtra = "extra"
)

// NetworkPerfMapper operations related  to mapping between virtual machines to network performance categories
type NetworkPerfMapper interface {
	// MapNetworkPerf gets the network performance category for the given
	MapNetworkPerf(ntwPerf string) (string, error)
}

// ZonePrice struct for displaying price information per zone
type ZonePrice struct {
	Zone  string  `json:"zone"`
	Price float64 `json:"price"`
}

// newZonePrice creates a new zone price struct and returns its pointer
func newZonePrice(zone string, price float64) *ZonePrice {
	return &ZonePrice{
		Zone:  zone,
		Price: price,
	}
}

// ProductDetails extended view of the virtual machine details
type ProductDetails struct {
	// Embedded struct!
	VmInfo

	// Burst this is derived for now
	Burst bool `json:"burst,omitempty"`

	// ZonePrice holds spot price information per zone
	SpotInfo []ZonePrice `json:"spotPrice,omitempty"`
}

// ProductDetailSource product details related set of operations
type ProductDetailSource interface {
	// GetProductDetails gathers the product details information known by telescope
	GetProductDetails(provider string, region string) ([]ProductDetails, error)
}

// newProductDetails creates a new ProductDetails struct and returns a pointer to it
func newProductDetails(vm VmInfo) *ProductDetails {
	pd := ProductDetails{}
	pd.VmInfo = vm
	pd.Burst = vm.IsBurst()
	return &pd
}

// ServiceDescriber represents a service; eg.: oke, eks
// Extend this interface with other operations if needed
type ServiceDescriber interface {
	// ServiceName abstracts the name assembly for the service
	ServiceName() string
}

// ImageDescriber is a placeholder interface for image information
// to be extended with other operations if needed
type ImageDescriber interface {
	// ImageName returns the image name
	ImageName() string
}

// Service represents a service supported by a given provider.
// it's intended to implement the ServiceDescriber interface
type Service struct {
	Service string `json:"service"`
}

// ServiceName returns the service name
func (s Service) ServiceName() string {
	return s.Service
}

// NewService creates a new servicedescriptor struct
func NewService(name string) Service {
	return Service{Service: name}
}

// ProviderDescriber describes a provider
type ProviderDescriber interface {
	// ProviderName returns the name of the provider
	ProviderName() string
}

// Provider represents a cloud provider
type Provider struct {
	Provider string    `json:"provider"`
	Services []Service `json:"services"`
}

// ProviderName returns the name of the provider
func (p Provider) ProviderName() string {
	return p.Provider
}

// NewProvider create new provider describer struct
func NewProvider(name string) Provider {
	return Provider{
		Provider: name,
	}
}

// Image represents an image
type Image struct {
	Image string `json:"image"`
}

// ImageName returns the name of the image
func (i Image) ImageName() string {
	return i.Image
}

// NewImage create new provider describer struct
func NewImage(name string) *Image {
	return &Image{
		Image: name,
	}
}

// Version represents a version
type Version struct {
	Version string `json:"versions"`
}

// VersionName returns the name of the version
func (v Version) VersionName() string {
	return v.Version
}