package main

import (
	"log"
	"math"
	"math/rand"

	"github.com/aiseeq/s2l/lib/point"
	"github.com/aiseeq/s2l/lib/scl"
	"github.com/aiseeq/s2l/protocol/api"
	"github.com/aiseeq/s2l/protocol/enums/ability"
	"github.com/aiseeq/s2l/protocol/enums/protoss"
	"github.com/aiseeq/s2l/protocol/enums/terran"
	"github.com/aiseeq/s2l/protocol/enums/zerg"
)

func filterInProgress(unit *scl.Unit) bool {
	return unit.BuildProgress < 1
}

func filterGatheringOrIdle(unit *scl.Unit) bool {
	if len(unit.Orders) == 0 {
		return true
	}
	return unit.Orders[0].AbilityId == ability.Harvest_Gather_SCV
}

func filterBuildSupplyDepotOrder(unit *scl.Unit) bool {
	if len(unit.Orders) == 0 {
		return false
	}

	for _, order := range unit.Orders {
		if order.AbilityId == ability.Build_SupplyDepot {
			return true
		}
	}

	return false
}

func (b *Bot) BuildSupplyDepot() {
	currSupply := b.Obs.PlayerCommon.FoodUsed
	maxSupply := b.Obs.PlayerCommon.FoodCap
	supplyLeft := maxSupply - currSupply

	ccs := b.Units.My.OfType(terran.CommandCenter, terran.OrbitalCommand, terran.PlanetaryFortress)
	if ccs.Empty() {
		return
	}

	// Kinda assume that all command centers are building SCVs at the same time
	// even though they're not, but that's just to estimate the production per
	// bases.
	timeForScv := float64(BuildTimeSCV) / float64(ccs.Len())
	scvDuringDepots := uint32(math.Ceil(float64(BuildTimeSupplyDepot) / timeForScv))

	depotsOrdered := b.Units.My.OfType(terran.SCV).Filter(filterBuildSupplyDepotOrder).Len()
	depotsInProgress := b.Units.My.OfType(terran.SupplyDepot).Filter(filterInProgress).Len()
	shouldBuildDepot := maxSupply < 200 && supplyLeft <= scvDuringDepots && depotsOrdered+depotsInProgress == 0

	if !shouldBuildDepot || !b.CanBuy(ability.Build_SupplyDepot) {
		return
	}

	workers := b.Units.My[terran.SCV].Filter(filterGatheringOrIdle)
	if len(workers) == 0 {
		return
	}

	builder := workers.First()
	if builder == nil {
		return
	}

	cc := ccs[rand.Intn(len(ccs))]

	// Find a good position for the supply depot
	pos := b.whereToBuild(cc.Point(), scl.S2x2, terran.SupplyDepot, ability.Build_SupplyDepot)
	if pos == nil {
		log.Printf("No valid position found for supply depot")
		return
	}

	log.Printf("Building supply depot at %v", *pos)
	builder.CommandPos(ability.Build_SupplyDepot, pos)

	// Then go back to mining afterwards
	if mineralField := b.Units.Minerals.All().ClosestTo(cc); mineralField != nil {
		log.Printf("and queuing to harvest at %v", mineralField.Point())
		builder.CommandTagQueue(ability.Smart, mineralField.Tag)
	}

	b.DeductResources(ability.Build_SupplyDepot)
}

// whereToBuild finds a valid position to place a building of the given size.
func (b *Bot) whereToBuild(start point.Point, size scl.BuildingSize, buildingType api.UnitTypeID, ability api.AbilityID) *point.Point {
	// maxDist is roughly the maximum size of a typical base. At which point, we
	// should probably just build somewhere else...
	const maxDist = 30.0

	// Try the exact starting position first
	startPos := start.Floor()
	if b.isValidBuildPosition(startPos, size, buildingType, ability) {
		return &startPos
	}

	// Spiral search around the starting position
	for dist := 1.0; dist <= maxDist; dist++ {
		// Top-left corner of the current ring
		topLeft := startPos.Add(-dist, +dist)

		// Top edge (left to right, x increasing)
		for x := 0.0; x < 2*dist; x++ {
			pos := topLeft.Add(+x, 0)
			if b.isValidBuildPosition(pos, size, buildingType, ability) {
				return &pos
			}
		}

		// Right edge (top to bottom, y decreasing)
		for y := 0.0; y < dist*2; y++ {
			pos := topLeft.Add(2*dist, -y)
			if b.isValidBuildPosition(pos, size, buildingType, ability) {
				return &pos
			}
		}

		// Bottom edge (right to left, x decreasing)
		for x := 0.0; x < 2*dist; x++ {
			pos := topLeft.Add(dist*2-x, -dist*2)
			if b.isValidBuildPosition(pos, size, buildingType, ability) {
				return &pos
			}
		}

		// Left edge (bottom to top, y increasing)
		for y := 0.0; y < dist*2; y++ {
			pos := topLeft.Add(0, -dist*2+y)
			if b.isValidBuildPosition(pos, size, buildingType, ability) {
				return &pos
			}
		}
	}

	// No valid position found
	return nil
}

// isValidBuildPosition checks if a position is valid for buildings of the
// specified size and type
func (b *Bot) isValidBuildPosition(pos point.Point, size scl.BuildingSize, buildingType api.UnitTypeID, ability api.AbilityID) bool {
	// Check if the position is buildable according to the grid
	//
	// TODO: Probably needs to check for burrowed units and other stuff
	if !b.IsPosOk(pos, size, 0, scl.IsBuildable, scl.IsPathable, scl.IsNoCreep) {
		return false
	}

	// Get nearby resources
	mineralField := b.Units.Minerals.All().
		CloserThan(scl.ResourceSpreadDistance, pos).Filter(HasMinerals).ClosestTo(pos)

	vespineGeyser := b.Units.Geysers.All().
		CloserThan(scl.ResourceSpreadDistance, pos).Filter(HasGas).ClosestTo(pos)

	gas := b.Units.My.OfType(
		terran.Refinery, terran.RefineryRich,
		protoss.Assimilator, protoss.AssimilatorRich,
		zerg.Extractor, zerg.ExtractorRich,
	).
		CloserThan(scl.ResourceSpreadDistance, pos).Filter(HasGas).ClosestTo(pos)

	resource := scl.Units{mineralField, vespineGeyser, gas}.Filter(IsNotNil).ClosestTo(pos)

	townHall := b.Units.My.OfType(terran.CommandCenter, terran.OrbitalCommand, terran.PlanetaryFortress).
		CloserThan(scl.ResourceSpreadDistance, pos).ClosestTo(pos)

	if resource != nil && townHall != nil {
		// When there's mineral fields and town halls nearby, make sure we're not
		// between them.
		//
		// The way it's calculated assumes that a point in the middle between the
		// mineral fields and the command center should be at a distance of
		// `townHall.Dist(mineralField) == mineralField.Dist(pos) + townHall.Dist(pos)`
		//
		// If the distance is equal or less than that + half the building size, it
		// means we're between them.
		if (resource.Dist(pos) + townHall.Dist(pos)) <= townHall.Dist(resource)+1 {
			log.Printf("Position %v is between mineral field %v and town hall %v", pos, resource.Tag, townHall.Tag)
			return false
		}
	}

	// Touchy buildings are buildings that can touch one other type of building
	touchyBuildings := []api.UnitTypeID{terran.SupplyDepot, terran.SupplyDepotLowered, terran.MissileTurret}
	isTouchy := false
	for _, touchy := range touchyBuildings {
		if touchy == buildingType {
			isTouchy = true
			break
		}
	}

	// Check all buildings near the target position
	myBuildings := b.Units.MyAll.Filter(IsStructure)

	// Collect touching building types. Ignores own type.
	touchingTypes := make(map[api.UnitTypeID]bool)
	for _, building := range myBuildings {
		maxDistance := sizeLength(buildingToSize(building)) + sizeLength(size)
		distance := building.Point().Dist(pos)

		// Building far enough and not the same type
		if distance < maxDistance && buildingType != building.UnitType {
			touchingTypes[building.UnitType] = true
		}
	}

	// Count how many different types are touching this building
	touchingTypeCount := len(touchingTypes)

	if !isTouchy && touchingTypeCount > 0 {
		log.Printf("Position %v is touching %d different building types", pos, touchingTypeCount)
		return false
	}

	if isTouchy && touchingTypeCount > 1 {
		log.Printf("Position %v is touching %d different building types", pos, touchingTypeCount)
		return false
	}

	return b.RequestPlacement(ability, pos, nil)
}

func sizeLength(size scl.BuildingSize) float64 {
	return map[scl.BuildingSize]float64{
		scl.S2x1: 2.0,
		scl.S2x2: 2.0,
		scl.S3x3: 3.0,
		scl.S5x3: 5.0,
		scl.S5x5: 5.0,
	}[size]
}

// wouldBuildingTouch checks if a new building at position would touch an
// existing building
func (b *Bot) wouldBuildingTouch(pos point.Point, size scl.BuildingSize, existingBuilding *scl.Unit) bool {
	existingPos := existingBuilding.Point().Floor()
	existingSize := buildingToSize(existingBuilding)

	return b.areSizesTouching(pos, size, existingPos, existingSize)
}

// areSizesTouching checks if two sizes at their positions are touching
func (b *Bot) areSizesTouching(pos1 point.Point, size1 scl.BuildingSize, pos2 point.Point, size2 scl.BuildingSize) bool {
	// Get all grid cells occupied by each building
	points1 := b.GetBuildingPoints(pos1, size1)
	points2 := b.GetBuildingPoints(pos2, size2)

	// Check if any cell from building 1 is adjacent to any cell from building 2
	for _, p1 := range points1 {
		for _, p2 := range points2 {
			// Pythagoras shortcut
			if p1.Dist(p2) <= math.Sqrt(2) {
				return true
			}
		}
	}

	return false
}

// areBuildingsTouching checks if two buildings are touching
func (b *Bot) areBuildingsTouching(building1 *scl.Unit, building2 *scl.Unit) bool {
	pos1 := building1.Point().Floor()
	size1 := buildingToSize(building1)

	pos2 := building2.Point().Floor()
	size2 := buildingToSize(building2)

	return b.areSizesTouching(pos1, size1, pos2, size2)
}

// buildingToSize converts a unit to a scl.BuildingSize
//
// See https://pkg.go.dev/github.com/aiseeq/s2l@v0.0.0-20210823112249-9c133fcb6b25/lib/scl#Bot.ParseUnits
func buildingToSize(u *scl.Unit) scl.BuildingSize {
	pos := u.Point()

	switch {
	case u.Radius <= 1:
		return 0

	case u.Radius >= 1.125 && u.Radius <= 1.25:
		pos -= point.Pt(1, 1)
		return scl.S2x2

	case u.Radius > 1.25 && u.Radius < 2.75:
		return scl.S3x3

	case u.Radius == 2.75:
		return scl.S5x5

	default:
		log.Printf("Unknown building size for %q: %f", u.UnitType, u.Radius)
	}

	return 0
}
