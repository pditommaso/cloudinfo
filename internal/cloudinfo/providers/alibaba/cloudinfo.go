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

package alibaba

import (
	"encoding/json"
	"fmt"
	"strings"

	"emperror.dev/emperror"
	"emperror.dev/errors"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/bssopenapi"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"

	"github.com/banzaicloud/cloudinfo/internal/cloudinfo"
	"github.com/banzaicloud/cloudinfo/internal/cloudinfo/metrics"
	"github.com/banzaicloud/cloudinfo/internal/cloudinfo/types"
	"github.com/banzaicloud/cloudinfo/internal/platform/log"
)

// AlibabaInfoer encapsulates the data and operations needed to access external Alibaba resources
type AlibabaInfoer struct {
	client CommonDescriber
	log    cloudinfo.Logger
}

const svcAck = "ack"

// NewAlibabaInfoer creates a new instance of the Alibaba infoer.
func NewAlibabaInfoer(config Config, logger cloudinfo.Logger) (*AlibabaInfoer, error) {
	client, err := sdk.NewClientWithAccessKey(
		config.Region,
		config.AccessKey,
		config.SecretKey,
	)
	if err != nil {
		return nil, err
	}

	// client.GetConfig().WithAutoRetry(true)
	client.GetConfig().WithGoRoutinePoolSize(100)
	client.GetConfig().WithEnableAsync(true)
	client.GetConfig().WithDebug(true)
	client.GetConfig().WithMaxRetryTime(10)

	return &AlibabaInfoer{
		client: client,
		log:    logger,
	}, nil
}

// Initialize is not needed on Alibaba because price info is changing frequently
func (a *AlibabaInfoer) Initialize() (map[string]map[string]types.Price, error) {
	return nil, nil
}

func (a *AlibabaInfoer) getCurrentSpotPrices(region string) (map[string]types.SpotPriceInfo, error) {
	logger := log.WithFields(a.log, map[string]interface{}{"region": region})
	logger.Debug("start retrieving spot price data")
	priceInfo := make(map[string]types.SpotPriceInfo)

	zones, err := a.getZones(region)
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		for _, instanceType := range zone.AvailableInstanceTypes.InstanceTypes {
			if priceInfo[instanceType] == nil {
				describeSpotPriceHistory, err := a.client.ProcessCommonRequest(a.describeSpotPriceHistoryRequest(region, instanceType))
				if err != nil {
					logger.Error("failed to get spot price history", map[string]interface{}{"instancetype": instanceType})
					continue
				}

				response := &ecs.DescribeSpotPriceHistoryResponse{}

				err = json.Unmarshal(describeSpotPriceHistory.BaseResponse.GetHttpContentBytes(), response)
				if err != nil {
					return nil, err
				}

				spotPrice := make(types.SpotPriceInfo, 0)

				priceTypes := response.SpotPrices.SpotPriceType
				for _, priceType := range priceTypes {
					if zone.ZoneId == priceType.ZoneId {
						spotPrice[zone.ZoneId] = priceType.SpotPrice
						break
					}
					priceInfo[instanceType] = spotPrice
				}
			}
		}
	}
	logger.Debug("retrieved spot price data")
	return priceInfo, nil
}

func (a *AlibabaInfoer) getZones(region string) ([]ecs.Zone, error) {
	describeZones, err := a.client.ProcessCommonRequest(a.describeZonesRequest(region))
	if err != nil {
		return nil, emperror.Wrap(err, "DescribeZones API call problem")
	}

	response := &ecs.DescribeZonesResponse{}

	err = json.Unmarshal(describeZones.BaseResponse.GetHttpContentBytes(), response)
	if err != nil {
		return nil, err
	}

	return response.Zones.Zone, nil
}

func (a *AlibabaInfoer) GetVirtualMachines(region string) ([]types.VMInfo, error) {
	logger := log.WithFields(a.log, map[string]interface{}{"region": region})
	logger.Debug("getting product info")
	vms := make([]types.VMInfo, 0)

	instanceTypes, err := a.getInstanceTypes()
	if err != nil {
		return nil, err
	}

	availableZones, err := a.getZones(region)
	if err != nil {
		return nil, err
	}

	for _, instanceType := range instanceTypes {
		zones := make([]string, 0)
		for _, zone := range availableZones {
			for _, resourcesInfo := range zone.AvailableResources.ResourcesInfo {
				for _, availableInstanceType := range resourcesInfo.InstanceTypes.SupportedInstanceType {
					if availableInstanceType == instanceType.InstanceTypeId {
						zones = append(zones, zone.ZoneId)
						break
					}
				}
			}
		}
		if len(zones) > 0 {
			ntwMapper := newAlibabaNetworkMapper()
			ntwPerf := fmt.Sprintf("%.1f Gbit/s", float64(instanceType.InstanceBandwidthRx)/1024000)
			ntwPerfCat, err := ntwMapper.MapNetworkPerf(ntwPerf)
			if err != nil {
				logger.Debug(emperror.Wrap(err, "failed to get network performance category").Error(),
					map[string]interface{}{"instanceType": instanceType.InstanceTypeId})
			}

			category, err := a.mapCategory(instanceType.InstanceTypeId)
			if err != nil {
				logger.Debug(emperror.Wrap(err, "failed to get virtual machine category").Error(),
					map[string]interface{}{"instanceType": instanceType.InstanceTypeId})
			}

			vms = append(vms, types.VMInfo{
				Category:   category,
				Type:       instanceType.InstanceTypeId,
				Cpus:       float64(instanceType.CpuCoreCount),
				Mem:        instanceType.MemorySize,
				Gpus:       float64(instanceType.GPUAmount),
				NtwPerf:    ntwPerf,
				NtwPerfCat: ntwPerfCat,
				Zones:      zones,
				Attributes: cloudinfo.Attributes(fmt.Sprint(instanceType.CpuCoreCount), fmt.Sprint(instanceType.MemorySize), ntwPerfCat, category),
			})
		}
	}

	virtualMachines, err := a.getOnDemandPrice(vms, region)
	if err != nil {
		return nil, err
	}

	logger.Debug("found vms", map[string]interface{}{"numberOfVms": len(virtualMachines)})
	return virtualMachines, nil
}

// GetProducts retrieves the available virtual machines based on the arguments provided
func (a *AlibabaInfoer) GetProducts(vms []types.VMInfo, service, regionId string) ([]types.VMInfo, error) {
	var vmList = vms
	if len(vmList) == 0 {
		var err error
		vmList, err = a.GetVirtualMachines(regionId)
		if err != nil {
			a.log.Warn("could not get machine types for region", map[string]interface{}{"regionId": regionId})
			return nil, emperror.Wrap(err, "failed to get products")
		}
	}
	switch service {
	case svcAck:
		return vmList, nil

	case "compute":
		return vmList, nil

	default:
		return nil, errors.Wrap(errors.New(service), "invalid service")
	}
}

func (a *AlibabaInfoer) getInstanceTypes() ([]ecs.InstanceType, error) {
	describeInstanceTypes, err := a.client.ProcessCommonRequest(a.describeInstanceTypesRequest())
	if err != nil {
		return nil, emperror.Wrap(err, "DescribeInstanceTypes API call problem")
	}

	response := &ecs.DescribeInstanceTypesResponse{}

	err = json.Unmarshal(describeInstanceTypes.BaseResponse.GetHttpContentBytes(), response)
	if err != nil {
		return nil, err
	}

	return response.InstanceTypes.InstanceType, nil
}

func (a *AlibabaInfoer) getOnDemandPrice(vms []types.VMInfo, region string) ([]types.VMInfo, error) {
	allPrices := make(map[string]float64, 0)
	vmsWithPrice := make([]types.VMInfo, 0)
	var (
		prices []float64
		err    error
	)

	instanceTypes := make([]string, 0)

	for index, vm := range vms {
		instanceTypes = append(instanceTypes, vm.Type)

		if len(instanceTypes) == 25 || index+1 == len(vms) {
			prices, err = a.getPrice(instanceTypes, region)
			if err != nil {
				if err.Error() == "failed to get price" && hasLabel(emperror.Context(err), "InvalidParameter") {
					for i := 0; i < len(instanceTypes); i++ {
						prices, err = a.getPrice([]string{instanceTypes[i]}, region)
						if err != nil {
							a.log.Debug("no price for instance type", map[string]interface{}{"instanceType": instanceTypes[i]})
							continue
						}
						allPrices[instanceTypes[i]] = prices[0]
					}
				} else {
					return nil, err
				}
			} else {
				for i, price := range prices {
					allPrices[instanceTypes[i]] = price
				}
			}

			instanceTypes = make([]string, 0)
		}
	}

	for _, vm := range vms {
		vmsWithPrice = append(vmsWithPrice, types.VMInfo{
			Category:      vm.Category,
			Type:          vm.Type,
			OnDemandPrice: allPrices[vm.Type],
			Cpus:          vm.Cpus,
			Mem:           vm.Mem,
			Gpus:          vm.Gpus,
			NtwPerf:       vm.NtwPerf,
			NtwPerfCat:    vm.NtwPerfCat,
			Zones:         vm.Zones,
			Attributes:    vm.Attributes,
		})
	}

	return vmsWithPrice, nil
}

func (a *AlibabaInfoer) getPrice(instanceTypes []string, region string) ([]float64, error) {
	response := &bssopenapi.GetPayAsYouGoPriceResponse{}
	var price []float64

	getPayAsYouGoPrice, err := a.client.ProcessCommonRequest(a.getPayAsYouGoPriceRequest(region, instanceTypes))
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(getPayAsYouGoPrice.BaseResponse.GetHttpContentBytes(), response)
	if err != nil {
		return nil, err
	}

	if !response.Success {
		return nil, emperror.With(errors.New("failed to get price"), response.Code)
	}

	for _, moduleDetail := range response.Data.ModuleDetails.ModuleDetail {
		price = append(price, moduleDetail.OriginalCost)
	}

	return price, nil
}

// GetZones returns the availability zones in a region
func (a *AlibabaInfoer) GetZones(region string) ([]string, error) {
	logger := log.WithFields(a.log, map[string]interface{}{"region": region})
	logger.Debug("getting zones")

	var zones []string

	availableZones, err := a.getZones(region)
	if err != nil {
		return nil, err
	}

	for _, zone := range availableZones {
		zones = append(zones, zone.ZoneId)
	}

	logger.Debug("found zones", map[string]interface{}{"numberOfZones": len(zones)})
	return zones, nil
}

// GetRegions returns a map with available regions
func (a *AlibabaInfoer) GetRegions(service string) (map[string]string, error) {
	logger := log.WithFields(a.log, map[string]interface{}{"service": service})
	logger.Debug("getting regions")

	describeRegions, err := a.client.ProcessCommonRequest(a.describeRegionsRequest())
	if err != nil {
		return nil, emperror.Wrap(err, "DescribeRegions API call problem")
	}

	response := &ecs.DescribeRegionsResponse{}

	err = json.Unmarshal(describeRegions.BaseResponse.GetHttpContentBytes(), response)
	if err != nil {
		return nil, err
	}

	var regionIdMap = make(map[string]string)
	for _, region := range response.Regions.Region {
		regionIdMap[region.RegionId] = region.LocalName
	}

	logger.Debug("found regions", map[string]interface{}{"numberOfRegions": len(regionIdMap)})
	return regionIdMap, nil
}

// HasShortLivedPriceInfo - Spot Prices are changing continuously on Alibaba
func (a *AlibabaInfoer) HasShortLivedPriceInfo() bool {
	return false
}

// GetCurrentPrices returns the current spot prices of every instance type in every availability zone in a given region
func (a *AlibabaInfoer) GetCurrentPrices(region string) (map[string]types.Price, error) {
	var spotPrices map[string]types.SpotPriceInfo
	var err error

	spotPrices, err = a.getCurrentSpotPrices(region)
	if err != nil {
		return nil, err
	}

	prices := make(map[string]types.Price)
	for instanceType, sp := range spotPrices {
		prices[instanceType] = types.Price{
			SpotPrice:     sp,
			OnDemandPrice: -1,
		}
		for zone, price := range sp {
			metrics.ReportAlibabaSpotPrice(region, zone, instanceType, price)
		}
	}

	return prices, nil
}

// HasImages - Alibaba support images
func (a *AlibabaInfoer) HasImages() bool {
	return true
}

// GetServiceImages retrieves the images supported by the given service in the given region
func (a *AlibabaInfoer) GetServiceImages(service, region string) ([]types.Image, error) {
	describeImages, err := a.client.ProcessCommonRequest(a.describeImagesRequest(region))
	if err != nil {
		return nil, emperror.Wrap(err, "DescribeImages API call problem")
	}

	response := &ecs.DescribeImagesResponse{}

	err = json.Unmarshal(describeImages.BaseResponse.GetHttpContentBytes(), response)
	if err != nil {
		return nil, err
	}

	var images []types.Image

	for _, image := range response.Images.Image {
		if strings.Contains(image.ImageId, "centos_7") {
			images = append(images, types.Image{
				Name: image.ImageId,
			})
		}
	}

	return images, nil
}

// GetServiceProducts retrieves the products supported by the given service in the given region
func (a *AlibabaInfoer) GetServiceProducts(region, service string) ([]types.ProductDetails, error) {
	return nil, errors.New("GetServiceProducts - not yet implemented")
}

// GetVersions retrieves the kubernetes versions supported by the given service in the given region
func (a *AlibabaInfoer) GetVersions(service, region string) ([]types.LocationVersion, error) {
	switch service {
	case svcAck:
		return []types.LocationVersion{types.NewLocationVersion(region, []string{"1.16.6", "1.14.8"}, "1.14.8")}, nil
	default:
		return []types.LocationVersion{}, nil
	}
}

func hasLabel(ctx []interface{}, s interface{}) bool {
	for _, e := range ctx {
		if e == s {
			return true
		}
	}
	return false
}
