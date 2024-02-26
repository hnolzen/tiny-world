package sys

import (
	"math"

	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
	"github.com/mlange-42/tiny-world/game/comp"
	"github.com/mlange-42/tiny-world/game/nav"
	"github.com/mlange-42/tiny-world/game/res"
	"github.com/mlange-42/tiny-world/game/terr"
)

// Haul system.
type Haul struct {
	rules    generic.Resource[res.Rules]
	update   generic.Resource[res.UpdateInterval]
	stock    generic.Resource[res.Stock]
	landUse  generic.Resource[res.LandUse]
	landUseE generic.Resource[res.LandUseEntities]

	prodFilter      generic.Filter2[comp.Tile, comp.Production]
	warehouseFilter generic.Filter1[comp.Tile]
	filter          generic.Filter2[comp.Tile, comp.Hauler]

	haulerMap     generic.Map2[comp.Tile, comp.Hauler]
	homeMap       generic.Map2[comp.Tile, comp.Production]
	haulerBuilder generic.Map3[comp.Tile, comp.Hauler, comp.HaulerSprite]
	productionMap generic.Map1[comp.Production]

	aStar nav.AStar

	warehouses []comp.Tile
	toCreate   []markerEntry
	arrived    []ecs.Entity

	haulerSprites [terr.EndTerrain]int
}

// Initialize the system
func (s *Haul) Initialize(world *ecs.World) {
	s.rules = generic.NewResource[res.Rules](world)
	s.update = generic.NewResource[res.UpdateInterval](world)
	s.stock = generic.NewResource[res.Stock](world)
	s.landUse = generic.NewResource[res.LandUse](world)
	s.landUseE = generic.NewResource[res.LandUseEntities](world)

	s.prodFilter = *generic.NewFilter2[comp.Tile, comp.Production]()
	s.warehouseFilter = *generic.NewFilter1[comp.Tile]().With(generic.T[comp.Warehouse]())
	s.filter = *generic.NewFilter2[comp.Tile, comp.Hauler]()

	s.haulerMap = generic.NewMap2[comp.Tile, comp.Hauler](world)
	s.homeMap = generic.NewMap2[comp.Tile, comp.Production](world)
	s.haulerBuilder = generic.NewMap3[comp.Tile, comp.Hauler, comp.HaulerSprite](world)
	s.productionMap = generic.NewMap1[comp.Production](world)

	s.aStar = nav.NewAStar(s.landUse.Get())

	spritesRes := generic.NewResource[res.Sprites](world)
	sprites := spritesRes.Get()
	for i := terr.Terrain(0); i < terr.EndTerrain; i++ {
		s.haulerSprites[i] = sprites.GetIndex("hauler_" + terr.Properties[i].Name)
	}
}

// Update the system
func (s *Haul) Update(world *ecs.World) {
	rules := s.rules.Get()
	update := s.update.Get()
	landUse := s.landUse.Get()
	stock := s.stock.Get()

	prodQuery := s.prodFilter.Query(world)
	for prodQuery.Next() {
		tile, prod := prodQuery.Get()
		if prod.Stock < rules.HaulerCapacity || prod.IsHauling {
			continue
		}
		s.toCreate = append(s.toCreate, markerEntry{Tile: *tile, Resource: prod.Type, Home: prodQuery.Entity()})
	}

	query := s.filter.Query(world)
	for query.Next() {
		tile, haul := query.Get()

		haul.PathFraction++
		if haul.Index <= 1 && haul.PathFraction >= uint8(update.Interval/2-1) {
			s.arrived = append(s.arrived, query.Entity())
			continue
		}

		if haul.PathFraction < uint8(update.Interval) {
			continue
		}
		haul.PathFraction = 0

		haul.Index--
		last := haul.Path[haul.Index]
		tile.X, tile.Y = last.X, last.Y

		if haul.Index <= 0 || (haul.Index <= 1 && haul.PathFraction >= uint8(update.Interval/2-1)) {
			s.arrived = append(s.arrived, query.Entity())
		}
	}

	if len(s.toCreate) > 0 {
		query := s.warehouseFilter.Query(world)
		for query.Next() {
			s.warehouses = append(s.warehouses, *query.Get())
		}
	}

	for _, entry := range s.toCreate {
		var bestPath []comp.Tile
		bestPathLen := math.MaxInt
		for _, tile := range s.warehouses {
			if path, ok := s.aStar.FindPath(entry.Tile, tile); ok {
				if len(path) < bestPathLen {
					bestPathLen = len(path)
					bestPath = path
				}
			}
		}
		if len(bestPath) == 0 {
			continue
		}
		luHere := landUse.Get(entry.Tile.X, entry.Tile.Y)

		prod := s.productionMap.Get(entry.Home)
		prod.Stock -= rules.HaulerCapacity
		prod.IsHauling = true
		s.haulerBuilder.NewWith(
			&entry.Tile,
			&comp.Hauler{
				Hauls:        entry.Resource,
				Home:         entry.Home,
				Path:         bestPath,
				PathFraction: uint8(update.Interval/2) + 1,
				Index:        len(bestPath) - 1,
			},
			&comp.HaulerSprite{
				SpriteIndex: s.haulerSprites[luHere],
			},
		)
	}

	for _, e := range s.arrived {
		tile, haul := s.haulerMap.Get(e)

		if !world.Alive(haul.Home) {
			world.RemoveEntity(e)
			continue
		}
		target := haul.Path[0]

		home, prod := s.homeMap.Get(haul.Home)
		if landUse.Get(target.X, target.Y) == terr.Warehouse {
			stock.Res[haul.Hauls] += rules.HaulerCapacity

			path, ok := s.aStar.FindPath(target, *home)
			if !ok {
				prod.IsHauling = false
				world.RemoveEntity(e)
			}
			haul.Path = path
			haul.Index = len(path) - 1
			haul.PathFraction = uint8(update.Interval/2) + 1
			*tile = target
			continue
		}

		prod.IsHauling = false
		world.RemoveEntity(e)
	}

	s.warehouses = s.warehouses[:0]
	s.toCreate = s.toCreate[:0]
	s.arrived = s.arrived[:0]
}

// Finalize the system
func (s *Haul) Finalize(world *ecs.World) {}