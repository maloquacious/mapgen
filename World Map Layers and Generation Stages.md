# World Map Layers and Generation Stages

This reference defines the categories of information in a generated game world and explains how they support generation in stages. A user can keep accepted results through stage X, try different seeds for a later stage Y, and compare the resulting alternatives without losing the accepted work.

The categories below describe what the world contains. Generation stages describe how that information is produced. A category does not have to correspond to exactly one stage.

## World requirements

- Earth-like worlds with 30–35 percent land.
- A physical resolution of 10–25 km per cell.
- A rectangular playable map that wraps east to west, with no movement across the northern or southern boundaries. This is a cylindrical map.
- Generation may use an intermediate map that wraps in both directions, called a toroidal map, provided the final world works with the playable boundaries.
- Convincing continental shapes and mountain systems, coherent climate and ecosystems, and mineral resources.
- Practical generation time and the ability to regenerate individual stages.

The exact meaning of convincing mountains and the acceptable runtime remain to be defined. This reference establishes vocabulary and workflow; it does not replace the existing implementation specifications or choose a mountain-generation algorithm.

## Map categories

### Topography

Topography describes the height and shape of the underlying ground, including the seabed. Elevation is height relative to a reference such as sea level. Local relief describes the difference between high and low ground within an area; slope describes how quickly height changes with distance.

Examples: mean ground elevation, ocean depth, steep slopes, and high relief within a mountain cell.

Elevation alone does not identify a landform. A high plateau may have little local relief, while a lower mountain range may have substantial relief.

### Landforms

Landforms describe the physical forms of the terrain.

Examples: mountains, hills, plains, plateaus, valleys, basins, ridges, and foothills.

A plain describes relatively flat terrain, not its vegetation. It may support forest, grassland, desert vegetation, or wetlands. Mountains may be forested, barren, or ice-covered.

### Climate

Climate describes long-term atmospheric conditions and their seasonal patterns.

Examples: temperature, precipitation, wet and dry seasons, winter cold, and prevailing winds.

Climate should be represented by the values needed by the game and generator, with optional descriptive classes such as hot and humid, temperate and seasonal, or cold and dry. It is distinct from the weather on a particular day.

### Hydrology

Hydrology describes where water collects, how it flows, and the connections between water features.

Examples: rivers, lakes, drainage basins, downstream flow, runoff, and wetland conditions.

Ocean and lake extent describe where water is present; drainage describes how water reaches those places. Freshwater is a water property, not a landform. Lakes can also be saline.

A swamp is a forested wetland. For generation, represent its wetland conditions and tree cover separately, then derive the descriptive label “swamp.”

### Biomes

A biome describes a broad ecological setting associated with climate and characteristic living communities. It is more specific than simply describing what visibly covers the ground.

Examples: tropical rainforest, temperate deciduous forest, temperate grassland, boreal forest, tundra, and desert.

“Forest” is a broad description of vegetation cover. “Boreal forest” identifies a more specific ecological setting. “Plains” is a landform; “grassland” describes vegetation or a biome, depending on the classification used.

### Surface cover

Surface cover describes what physically covers the surface. In standard usage, land cover includes vegetation, water, bare materials, snow, and ice. For this generator, separate these into overlapping components rather than placing them all in one exclusive list.

| Component | Examples | Relationship to other categories |
| --- | --- | --- |
| Vegetation cover | Trees, grass, shrubs, sparse vegetation | Describes actual cover within a broader biome |
| Exposed ground | Bare rock, sand, exposed soil | Can coexist with vegetation within a cell |
| Water cover | Ocean, lake water, river channels | Obtains extent and connections from hydrology |
| Snow and ice | Seasonal snow, persistent snowfields, glaciers, sea ice | Can overlay ground or water; glaciers are distinct ice features |

Snow-covered forest remains forest. A glacier may cover mountainous ground. A frozen lake remains a lake with ice over its water. The term “cryosphere” refers collectively to frozen water, including snow and ice; “snow and ice” is sufficient as a layer name here.

### Geology and mineral resources

Geology describes the underlying rocks and geological setting. Mineral resources describe occurrences that may matter to the game.

Examples: volcanic rocks, sedimentary basins, geological provinces, iron ore deposits, copper deposits, and gold occurrences.

Keep resource occurrence separate from resource discovery, mining, and ownership. Those are game-state concerns. Resource placement can use geological context without requiring a full geological simulation.

#### Rock types

Each cell has one dominant rock type, including cells under water.

| Rock type | Typical setting |
| --- | --- |
| Igneous | Near plate boundaries, reaching further inland at convergent boundaries, and in scattered volcanic provinces |
| Metamorphic | Along boundaries where plates collide or slide past each other |
| Sedimentary | Quiet plate interiors and basins |

Chosen in this order:

|    Rock     |                                                  Rule                                                  | Share of land |
|-------------|--------------------------------------------------------------------------------------------------------|---------------|
| Metamorphic | (\|pressure\| + \|shear\|)/2 + (2·rv − 1) >= limit (limit is 0.09 at transform boundaries, 0.10 otherwise) | 42%           |
| Igneous     | otherwise, if rv > 0.4 or igneous_chance > 0.25                                                        | 37%           |
| Sedimentary | everything else                                                                                        | 21%           |

- Inputs: pressure and shear come from how hard plates push against or slide along nearby boundaries, and fade with distance.
  rv is 4-octave noise, scaled and squared. igneous_chance is high close to a boundary and reaches further out at convergent boundaries.
- In practice: metamorphic rock follows stressed boundaries, igneous rock sits near boundaries and in noise patches, and sedimentary rock fills the quiet interiors.

#### Ore types

A cell holds at most one ore, or none. Most cells hold none. Which ores can occur depends on the cell's rock type:

| Ore | Sedimentary | Igneous | Metamorphic |
| --- | --- | --- | --- |
| Coal | Yes | | |
| Copper | Yes | Yes | Yes |
| Tin | Yes | Yes | |
| Iron | Yes | Yes | |
| Gold | Yes | | Yes |
| Diamond | Yes | Yes | Yes |
| Platinum | | Yes | |
| Aluminum | | Yes | |
| Silver | | Yes | Yes |
| Lead | | | Yes |

In code, keep "unclassified" (not yet generated) distinct from "none" (generated, no ore present) for both rock and ore.

A cell has no ore when ov is between 0.325 and 0.675. Otherwise the ore depends on the rock type and on ov2 = (ov + rv)/2, taking the first row where ov2 is below the limit:

|  Sedimentary  |     Igneous     |  Metamorphic  |
|---------------|-----------------|---------------|
| < 0.28 coal   | < 0.22 copper   | < 0.28 copper |
| < 0.35 copper | < 0.28 platinum | < 0.36 lead   |
| < 0.45 tin    | < 0.37 aluminum | < 0.51 silver |
| < 0.50 iron   | < 0.46 iron     | < 0.62 gold   |
| < 0.58 gold   | < 0.55 silver   | < 0.67 none   |
| else diamond  | < 0.62 tin      | else diamond  |
|               | else diamond    |               |

That gives 10 ores in total: coal, copper, tin, iron, gold, diamond, platinum, aluminum, silver and lead.

- Frequency: 16% of land cells carry ore. Silver, aluminum and tin are the most common; platinum and lead are the rarest.
- Ocean: ocean cells get ore too.
- Planned type: the doc says to make ore a typed enum where zero means "unclassified" and a separate value means "none".
- Odd gap: the metamorphic "none" band from 0.62 to 0.67 is copied from the source. The doc says to keep it but note that it may not be intended.


## Examples of overlapping layers

A cell has several properties at once. These examples illustrate combinations, not exhaustive terrain classes.

| Example area | Landform | Climate | Biome | Surface cover and water | Resource example |
| --- | --- | --- | --- | --- | --- |
| Snowy mountain region | Mountains and valleys | Cold, wet | Alpine tundra | Sparse plants, bare rock, seasonal snow, streams | Copper occurrence |
| Forested lowland | Plain | Warm, wet | Tropical rainforest | Trees, rivers, patches of forested wetland | No assigned deposit |
| Grassy upland | Plateau | Temperate, seasonally dry | Temperate grassland | Grass, exposed rock, a freshwater lake | Iron occurrence |
| Glaciated range | Mountains | Cold enough for persistent ice | Alpine tundra on exposed ground | Glaciers, rock, snow, meltwater | No assigned deposit |
| Frozen sea | Seabed basin beneath ocean | Polar | Marine classification, if modeled | Ocean water with sea ice | No assigned deposit |

At 10–25 km per cell, a cell represents an area containing multiple features. Use a dominant category where a simple label is sufficient, and coverage fractions or feature geometry where needed. A river can cross a forest cell without turning the entire cell into open water. An individual peak need not become the elevation of the whole cell.

## Generation in stages

Each stage consumes explicit inputs and produces a saved result. Give each randomized stage its own seed so changing a later seed does not change earlier results.

The following is a starting workflow, not a fixed algorithm:

| Stage | Main result | Example of what the user can vary |
| --- | --- | --- |
| 1. World layout | Continents, islands, ocean and land distribution | Continental seed while retaining world dimensions and target land fraction |
| 2. Geological structure and terrain | Geological regions, rock types, mountain systems, elevation and relief | Mountain layout while retaining accepted continents |
| 3. Drainage preparation | Drainage routes, basins, candidate lakes, any required terrain corrections | Drainage alternatives while retaining the accepted terrain input |
| 4. Climate and water balance | Climate, runoff, river flow, lake and wetland conditions | Climate alternatives while retaining accepted terrain and drainage |
| 5. Ecosystems and surface cover | Biomes, vegetation, exposed ground, snow and ice | Ecological variation consistent with accepted physical conditions |
| 6. Mineral resources | Ore occurrences, chosen by rock type | Deposit placement while retaining accepted geology and surface world |

Stages have dependencies, not merely a numerical order. Resource placement may depend on geology without depending on vegetation. Snow cover depends on climate; drainage routes depend on terrain; river flow also depends on precipitation. A stage need not use randomness if its inputs determine its result.

Some processes interact. Drainage preparation may need to alter elevation, and glacier erosion may alter landforms. Such changes must be explicit outputs of an appropriate stage. A later stage must not silently rewrite an accepted earlier result. Coupled processes can instead be resolved together before accepting that stage, or offered as an explicit revision that invalidates affected descendants.

## Keeping and comparing results

1. **Accept a checkpoint.** Save the stage output, its inputs, seed, settings, and generator version.
2. **Generate alternatives.** Run a later stage with another seed or settings against the same accepted inputs.
3. **Preserve earlier work.** Keep the accepted checkpoint and existing alternatives available for comparison.
4. **Refresh affected results.** Recompute or mark stale every downstream result that depends on a changed output. Independent results can remain valid.
5. **Reproduce a choice.** The same inputs, stage seed, settings, and generator version must reproduce the same result. Rerunning an unchanged stage must not depend on how many random choices another stage made.

For example, a user accepts the continents and mountain systems, then compares three climate seeds. All three alternatives retain the same terrain. Their vegetation, snow, and river flow must be generated from the respective climate result. Deposits based only on the accepted geology can remain unchanged.

If the user instead changes the mountain stage, terrain-dependent drainage, climate, and ecosystems become stale. The accepted continental layout remains available.

Seed variation must still respect the inputs: changing a biome seed should produce plausible ecological alternatives within the accepted climate, rather than arbitrary biome changes. Deterministic stages may provide variation through settings or explicit alternatives instead of a seed.
