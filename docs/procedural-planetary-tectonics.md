# Procedural Planetary Tectonics: Implementation Reference

Implementation reference distilled from Cortial, Peytavie, Galin, Guérin, *Procedural Tectonic Planets*, Computer Graphics Forum 38(2), 2019, DOI [10.1111/cgf.13614](https://doi.org/10.1111/cgf.13614).
The full transcription, with figures and rationale, is [research/2019-Procedural-Tectonic-Planets.md](research/2019-Procedural-Tectonic-Planets.md).
Equation numbers (Eq. n) match that transcription.

Conventions in this document:

- **Paper** marks behavior the paper states.
- **Unspecified** marks something the paper leaves open; an implementation must choose, and should record the choice.
- Numerical constants are calibration values from the paper's examples, not contracts.

## 1. Domain

| Item | Definition |
| --- | --- |
| Planet | Sphere of radius $R$ centered at the origin. Oblateness ignored. |
| Point | $\mathbf{p} \in \mathbb{R}^3$, $\lVert\mathbf{p}\rVert = R$. |
| Surface normal | $\mathbf{n}(\mathbf{p}) = \mathbf{p} / \lVert\mathbf{p}\rVert$. |
| Distances | Geodesic (great-circle) distances on the sphere. |
| Sea level | $z = 0$. |
| Time step | $\delta t$ (2 My). Simulation time $t$ advances by $\delta t$ per iteration. |
| Run length | Plausible planets after $\approx 250$ My. The paper says "a few hundred iterations"; at $\delta t$ = 2 My that is about 125. |

### Sampling and mesh

- Samples: $N$ points from spherical Fibonacci sampling.
- Mesh $\mathcal{M}$: global spherical Delaunay triangulation of the samples, computed once and reused.
- Each plate is a subset of the triangulation's triangles; per-point fields are interpolated barycentrically inside triangles.
- Resolution: mean spacing $\approx \sqrt{4\pi R^2 / N}$.

| $N$ | Resolution ($R = 6370$ km) |
| ---: | ---: |
| 60 000 | 102 km |
| 100 000 | 78 km |
| 250 000 | 50 km |
| 500 000 | 35 km (paper default) |

## 2. Data model

### Planet

```
Planet {
    R        float            // radius, km
    t        float            // current time, My
    Samples  []Sample         // fixed sample positions on the reference sphere
    Mesh     Triangulation    // spherical Delaunay over Samples
    Plates   []Plate
}
```

### Plate $\mathcal{P}$

```
Plate {
    Triangles []TriangleIndex  // partition of Mesh; plates are disjoint and cover the sphere
    Points    []CrustSample    // crust data carried by the plate (moves with it)
    W         vec3             // rotation axis w, unit vector through the planet center
    Omega     float            // angular speed ω
    Centroid  vec3             // c_i, used by slab pull (Eq. 6) and rifting
    BVH       BoundingVolumeHierarchy  // over the plate's triangles, for intersection and distance queries
}
```

Geodetic movement $\mathcal{G} = (\mathbf{w}, \omega)$. Surface velocity of a point on the plate:

$$
s(\mathbf{p}) = \omega \, \mathbf{w} \times \mathbf{p}
$$

Per time step each plate's points rotate about $\mathbf{w}$ by angle $\omega\,\delta t$.

### Crust sample (Table 1)

| Field | Symbol | Type | Applies to | Meaning |
| --- | :---: | --- | --- | --- |
| Crust type | $x_{\mathcal{C}}$ | enum {Oceanic, Continental} | all | |
| Thickness | $e$ | float, km | all | Carried; no update rule given (**Unspecified**). |
| Elevation | $z$ | float, km | all | Relative to sea level. |
| Oceanic age | $a_o$ | float, My | oceanic | Time since formation at a ridge. |
| Ridge direction | $\mathbf{r}$ | vec3, tangent | oceanic | Local direction parallel to the ridge (Eq. 13). |
| Orogeny age | $a_c$ | float, My | continental | Time since last uplift (reset to 0 by subduction uplift). |
| Orogeny type | $o$ | enum {None, Andean, Himalayan} | continental | Andean = subduction; Himalayan = continental collision. |
| Fold direction | $\mathbf{f}$ | vec3, tangent | continental | Local fold direction (Eq. 5, Eq. 11). |

**Unspecified:** ages are presumably incremented by $\delta t$ every step; the paper does not say so explicitly.

### Terrane $\mathcal{R}$

A connected region of continental crust within one plate.
Sizes range from single islands to whole continents.
Attributes used: area $\mathcal{A}$, centroid $\mathbf{q}$.

### Terrain type $x_{\mathcal{T}}$ (amplification only)

enum {Oceanic, Plain, Andean, Himalayan, OldMountains}. Assignment in Section 9.

## 3. Constants (Appendix A)

| Symbol | Meaning | Value | Per step ($\delta t$ = 2 My) |
| :---: | --- | ---: | ---: |
| $\delta t$ | Time step | 2 My | |
| $R$ | Planet radius | 6 370 km | |
| $z_r$ | Highest oceanic ridge elevation | −1 km | |
| $z_a$ | Abyssal plain elevation | −6 km | |
| $z_t$ | Oceanic trench elevation (minimum) | −10 km | |
| $z_c$ | Highest continental altitude | 10 km | |
| $r_s$ | Maximum subduction distance | 1 800 km | |
| $r_c$ | Maximum collision distance | 4 200 km | |
| $\Delta_c$ | Collision coefficient | $1.3 \times 10^{-5}$ km⁻¹ | |
| $v_0$ | Maximum plate speed | 100 mm/y (= 100 km/My) | 200 km |
| $\varepsilon_o$ | Oceanic elevation damping | $4 \times 10^{-2}$ mm/y | 80 m |
| $\varepsilon_c$ | Continental erosion | $3 \times 10^{-2}$ mm/y | 60 m |
| $\varepsilon_t$ | Sediment accretion in trenches | $3 \times 10^{-1}$ mm/y | 600 m |
| $u_0$ | Base subduction uplift | $6 \times 10^{-1}$ mm/y | 1.2 km |

Other parameters named in the paper:

| Name | Value | Use |
| --- | --- | --- |
| Collision interpenetration threshold | 300 km | Triggers a continental collision event. |
| Resampling interval | every 10–60 steps (20–120 My), shorter at higher maximum plate speed | Oceanic crust generation, remeshing, rifting. |
| $\mathcal{A}_0$ | Average initial plate area ($4\pi R^2 / \text{plate count}$) | Eq. 7, Eq. 14. |
| $\beta$ | **Unspecified** | Fold direction update weight (Eq. 5). |
| $\varepsilon$ | **Unspecified**, $\varepsilon \ll 1$ | Slab pull weight (Eq. 6). |
| $\lambda_0$ | **Unspecified** | Mean rifting events per time window (Eq. 14). |
| Old-mountain age threshold | **Unspecified**, user-set | Amplification terrain type. |
| Example setup | 40 plates, land fraction 0.3 | Performance table configuration. |

Unit discipline: keep lengths in km and time in My. Then 1 mm/y = 1 km/My, so the mm/y constants above can be used directly as km/My.

## 4. Initialization

1. Generate $N$ Fibonacci points on the sphere of radius $R$; build the spherical Delaunay triangulation $\mathcal{M}$.
2. Choose plate count $n_p$; distribute centroids $\mathbf{c}_i$ over the sphere.
3. Assign each sample to the plate whose centroid is nearest by geodesic distance, optionally warped by low-frequency noise for irregular boundaries:
   $\text{plate}(\mathbf{p}) = \arg\min_i \left( d_{geo}(\mathbf{p}, \mathbf{c}_i) + \text{noise}(\mathbf{p}) \right)$.
   **Unspecified:** noise type and amplitude.
4. Assign crust type per plate or per region to reach the desired continental ratio; initialize $z$, $e$, ages, directions.
   **Unspecified:** initial elevation values; reasonable defaults are oceanic $z \approx z_a$ and continental $z$ slightly above 0.
5. Assign each plate a random rotation axis $\mathbf{w}_i$ (unit) and angular speed $\omega_i$ with surface speed bounded by $v_0$.

Alternative: initialize from real elevation data, with user-set plate movements.

## 5. Simulation loop

```
for each step:
    move plates                         // rotate each plate's points about w_i by ω_i·δt
    update boundary tracking            // §5.1
    for each convergent boundary:
        if continental-continental and interpenetration > 300 km:
            continental collision       // §7, discrete event
        else:
            subduction                  // §6, continuous, per step
    erosion, dampening, trench filling  // §10, every sample, every step
    add low-frequency coherent noise    // §10
    increment ages                      // Unspecified, see §2
    every K steps (K in 10..60):
        global resample + remesh        // §11
        oceanic crust generation        // §8
        rifting test per plate          // §9
```

Per-sample updates within a step are independent and may run in parallel.
**Unspecified:** order of operations within a step beyond what is listed; choose one and keep it fixed for determinism.

### 5.1 Boundary tracking

- Track intersections between boundary triangles of different plates, accelerated by each plate's BVH.
- Converging boundary triangles are tracked across steps.
- For each vertex of a tracked converging triangle, update its distance to the convergence front each step by overestimation:
  $d(\mathbf{p}, t + \delta t) = d(\mathbf{p}, t) + \lVert s(\mathbf{p}) \rVert \, \delta t$.
- If two continental triangles of different plates intersect, switch that contact to continental-collision tracking.

## 6. Subduction (Paper §4.1)

### 6.1 Which plate subducts

Given converging plates $\mathcal{P}_i$ and $\mathcal{P}_j$ at a boundary, decide the subducting plate (here always $\mathcal{P}_i$):

| Plate A | Plate B | Subducting plate |
| --- | --- | --- |
| Oceanic | Oceanic | The older one (greater $a_o$ at the contact) |
| Oceanic | Continental | The oceanic one |
| Continental | Continental | Partial (*forced*) subduction; becomes a continental collision once interpenetration exceeds the threshold |

Forced subduction also applies when high oceanic relief or small terranes on the subducting plate are carried into the front; it produces larger uplift via $h$ (Eq. 2).
**Unspecified:** choice of subducting plate in the continental-continental case.

### 6.2 Uplift on the overriding plate $\mathcal{P}_j$

For every point $\mathbf{p}$ of $\mathcal{P}_j$ with $d(\mathbf{p}) < r_s$:

| Quantity | Definition |
| --- | --- |
| $d(\mathbf{p})$ | Distance from $\mathbf{p}$ to the subduction front |
| $v(\mathbf{p})$ | $\lVert s_i(\mathbf{p}) - s_j(\mathbf{p}) \rVert$, relative speed |
| $\tilde{z}_i(\mathbf{p})$ | $(z_i(\mathbf{p}) - z_t)/(z_c - z_t)$, normalized elevation of the subducting plate (Eq. 3) |

$$
\tilde{u}_j(\mathbf{p}) = u_0 \, f(d(\mathbf{p})) \, g(v(\mathbf{p})) \, h(\tilde{z}_i(\mathbf{p}))
\tag{1}
$$

| Function | Definition |
| --- | --- |
| $f(d)$ | Piecewise cubic on $[0, r_s]$, maximum 1 at a control distance $d_c$, falling to 0 at $d = r_s$. **Unspecified:** $d_c$ and the exact cubics. |
| $g(v)$ | $v / v_0$ |
| $h(\tilde{z})$ | $\tilde{z}^2$ (Eq. 2) |

$z_i(\mathbf{p})$ is the elevation of the subducting plate at the front point nearest $\mathbf{p}$ (**Unspecified** in the paper; it is sampled "at $\mathbf{p}$" across the boundary).
Optional user weights scale the influence of $f$, $g$, $h$.

A candidate $f$, not from the paper: with $x = d/r_s$ and $x_c = d_c/r_s$, use a smoothstep rising on $[0, x_c]$ and falling on $[x_c, 1]$:
$f = S(x/x_c)$ for $x \le x_c$, $f = 1 - S((x - x_c)/(1 - x_c))$ for $x_c < x \le 1$, $f = 0$ beyond, with $S(u) = 3u^2 - 2u^3$.

Updates for each such point:

$$
z_j(\mathbf{p}, t + \delta t) = z_j(\mathbf{p}, t) + \tilde{u}_j(\mathbf{p}) \, \delta t
\tag{4}
$$

$$
\mathbf{f}_j(\mathbf{p}, t + \delta t) = \mathbf{f}_j(\mathbf{p}, t) + \beta \, (s_i(\mathbf{p}) - s_j(\mathbf{p})) \, \delta t
\tag{5}
$$

- Set $a_c = 0$ where the point is emerged (continental or newly above sea level).
- Set $o = \text{Andean}$ (implied by the paper's orogeny types).
- **Unspecified:** projecting $\mathbf{f}$ back onto the tangent plane; clamping $z \le z_c$. Both are advisable.

### 6.3 Slab pull on the subducting plate $\mathcal{P}_i$

With $\mathbf{q}_k$, $k = 0 \ldots n-1$, the sample points on $\mathcal{P}_i$'s subduction front and $\mathbf{c}_i$ its centroid:

$$
\mathbf{w}_i(t + \delta t) = \mathbf{w}_i(t) + \varepsilon \sum_{k=0}^{n-1} \frac{\mathbf{c}_i \times \mathbf{q}_k}{\lVert \mathbf{c}_i \times \mathbf{q}_k \rVert} \, \delta t
\tag{6}
$$

- Effect: rotates the plate toward its subduction front; long fronts dominate.
- **Unspecified:** renormalizing $\mathbf{w}_i$ (it is defined as a unit vector; renormalize after the update and keep $\omega_i$).

### 6.4 Trench

The subduction front on the subducting side is the oceanic trench; its samples receive sediment fill (Eq. 17).
**Unspecified:** how trench depth is first imposed; the depth bound is $z_t$.

## 7. Continental collision (Paper §4.2)

Discrete event, triggered when continental triangles of two plates interpenetrate by more than 300 km.

Inputs: plate $\mathcal{P}_i$ carrying terrane $\mathcal{R}$ (area $\mathcal{A}$, centroid $\mathbf{q}$), collided plate $\mathcal{P}_j$.

1. Influence radius (Eq. 7):

   $$
   r = r_c \, \sqrt{v(\mathbf{q}) / v_0} \; \mathcal{A} / \mathcal{A}_0
   $$

   **Unspecified:** clamping $r$; it exceeds $r_c$ when $\mathcal{A} > \mathcal{A}_0$.

2. For every point $\mathbf{p}$ in $\mathcal{R} \oplus \mathcal{B}(r)$ (the terrane dilated by $r$), with $d(\mathbf{p}, \mathcal{R})$ its distance to the terrane (0 inside):

   $$
   \Delta z(\mathbf{p}) = \Delta_c \, \mathcal{A} \, f(d(\mathbf{p}, \mathcal{R})), \qquad f(x) = \left(1 - (x/r)^2\right)^2 \text{ for } x < r, \; 0 \text{ otherwise}
   \tag{8, 9}
   $$

   $$
   z(\mathbf{p}, t + \delta t) = z(\mathbf{p}, t) + \Delta z(\mathbf{p})
   \tag{10}
   $$

3. Fold direction, with $\mathbf{n}$ the normal at $\mathbf{p}$:

   $$
   \mathbf{f}(\mathbf{p}, t + \delta t) = \left( \mathbf{n} \times \frac{\mathbf{p} - \mathbf{q}}{\lVert \mathbf{p} - \mathbf{q} \rVert} \right) \times \mathbf{n}
   \tag{11}
   $$

   This equals the tangential component of the unit vector from $\mathbf{q}$ to $\mathbf{p}$.

4. Set $o = \text{Himalayan}$ in the affected region (implied); **Unspecified:** whether $a_c$ is reset (by analogy with subduction, reset to 0).
5. Detach the terrane's triangles from $\mathcal{P}_i$ and attach them to $\mathcal{P}_j$ (suture). The terrane then moves with $\mathcal{P}_j$.

Units: $\Delta_c \mathcal{A}$ with $\mathcal{A}$ in km² gives km.

## 8. Oceanic crust generation (Paper §4.3)

Runs at each resampling (every 10–60 steps), on samples left uncovered between diverging plates.

For each new sample $\mathbf{p}$:

| Quantity | Definition |
| --- | --- |
| Ridge $\Gamma$ | Mid-line between the diverging plates. **Unspecified:** construction; the medial line of the gap is the natural choice. |
| $d_\Gamma(\mathbf{p})$ | Distance to the ridge |
| $d_{\mathcal{P}}(\mathbf{p})$ | Distance to the nearest plate |
| $\alpha$ | $d_\Gamma / (d_\Gamma + d_{\mathcal{P}})$; 0 on the ridge, 1 at the plate edge |
| $\bar{z}(\mathbf{p})$ | Elevation linearly interpolated between the border elevations of the two plates |
| $z_\Gamma(\mathbf{p})$ | Template ridge profile: $z_r$ at the ridge, descending toward $z_a$. **Unspecified:** profile shape. |
| $\mathbf{q}$ | Projection of $\mathbf{p}$ onto $\Gamma$ |

$$
z(\mathbf{p}) = \alpha \, \bar{z}(\mathbf{p}) + (1 - \alpha) \, z_\Gamma(\mathbf{p})
\tag{12}
$$

$$
\mathbf{r}(\mathbf{p}) = (\mathbf{p} - \mathbf{q}) \times \mathbf{p}
\tag{13}
$$

Also set: $x_{\mathcal{C}} = \text{Oceanic}$, $a_o = 0$ (**Unspecified**, implied by the definition of $a_o$), plate = nearest plate.

## 9. Plate rifting (Paper §4.4)

Discrete event splitting a plate $\mathcal{P}$ into $n \in [2, 4]$ plates.

Trigger probability per plate (Eq. 14):

$$
P = \lambda e^{-\lambda}, \qquad \lambda = \lambda_0 \, f(x_{\mathcal{P}}) \, \mathcal{A}/\mathcal{A}_0
$$

- $x_{\mathcal{P}}$: continental fraction of the plate's crust; $f$ a transfer function (**Unspecified**; increasing in $x_{\mathcal{P}}$ discourages supercontinents).
- $\mathcal{A}$: plate area.
- **Unspecified:** evaluation frequency (per step or per resampling) and the time window $\lambda_0$ refers to.

Procedure:

1. Choose $n$ uniformly in $[2, 4]$.
2. Place $n$ random centroids $\mathbf{c}_i$ inside $\mathcal{P}$.
3. Partition $\mathcal{P}$'s samples into Voronoi cells of $\{\mathbf{c}_i\}$, with noise-warped distances for irregular fracture lines $\Gamma$.
4. Give each new plate a random geodetic movement that diverges from its siblings.
   The paper moves fragments apart directly (not by tearing about a pole on the rift) to avoid immediate re-suturing.

The user may trigger rifting with a prescribed location, fracture line, and time.

## 10. Per-step elevation processes (Paper §4.5)

Applied to every sample every step:

| Process | Applies to | Update |
| --- | --- | --- |
| Continental erosion (Eq. 15) | Continental, $z > 0$ (**Unspecified** filter) | $z \leftarrow z - (z / z_c) \, \varepsilon_c \, \delta t$ |
| Oceanic dampening (Eq. 16) | Oceanic | $z \leftarrow z - (1 - z / z_t) \, \varepsilon_o \, \delta t$ |
| Trench sediment fill (Eq. 17) | Trench samples | $z \leftarrow z + \varepsilon_t \, \delta t$ |
| Coherent noise | All | Add low-frequency coherent noise. **Unspecified:** type, amplitude. |

Properties: erosion is proportional to height; dampening drives oceanic $z$ toward $z_t$ and vanishes there.

## 11. Resampling and remeshing (Paper §6)

Every 10–60 steps:

1. Reuse the precomputed sample set and triangulation $\mathcal{M}$ (fixed positions on the reference sphere).
2. For each fixed sample, find which moved plate's triangle contains it:
   - inside a plate: copy crust data by barycentric interpolation of that triangle's vertices;
   - in a gap between diverging plates: oceanic crust generation (§8);
   - covered by several plates: **Unspecified**; choose a deterministic rule (for example, the overriding plate).
3. Rebuild each plate's triangle set by partitioning $\mathcal{M}$ by sample assignment; rebuild BVHs.

Interpolating vectors ($\mathbf{f}$, $\mathbf{r}$): **Unspecified**; interpolate, then project to the tangent plane and normalize.

## 12. Amplification (Paper §5)

Converts the coarse crust $\mathcal{C}$ to a detailed terrain $\mathcal{T}$.

### Terrain type assignment

| Condition | $x_{\mathcal{T}}$ |
| --- | --- |
| Oceanic crust | Oceanic (procedural) |
| Continental, in an orogeny, $o$ = Andean, $a_c \le$ threshold | Andean |
| Continental, in an orogeny, $o$ = Himalayan, $a_c \le$ threshold | Himalayan |
| Continental, in an orogeny, $a_c >$ threshold | OldMountains |
| Continental, no orogeny | Plain |

### Oceanic: procedural

$z_{\mathcal{T}} = z + \text{Gabor}(\mathbf{p}; \text{orientation} = \mathbf{r}, \text{amplitude} \downarrow a_o) + \text{fBm}_{\text{high freq}}(\mathbf{p})$

- 3D Gabor noise oriented by $\mathbf{r}$ makes transform faults perpendicular to ridges.
- Amplitude is larger where crust is young (small $a_o$).
- **Unspecified:** frequencies and amplitude laws.

### Continental: exemplar primitives

1. Preprocess each exemplar heightfield $\mathcal{E}$ (paper: SRTM 90 m tiles): extract its mean fold direction $\mathbf{f}_{\mathcal{E}}$ and elevation range; tag it with a terrain type.
2. At each continental sample, pick an exemplar matching $x_{\mathcal{T}}$.
3. Place it at the sample with a small random offset; rotate it in the tangent plane to align $\mathbf{f}_{\mathcal{E}}$ with the sample's $\mathbf{f}$.
4. $z_{\mathcal{T}}(\mathbf{p}) = \sum_k w_k(\mathbf{p}) \, z_{\mathcal{E}_k}(\mathbf{p}) \big/ \sum_k w_k(\mathbf{p})$ over overlapping primitives.
   **Unspecified:** weight kernel $w_k$, and how exemplar elevation is rescaled to the sample's $z$.

## 13. Known limitations (from the paper)

- Produces more forced subductions than expected (large terranes are not prevented from subducting).
- No passive margins or shallow continental shelves.
- No hot spots (suggested: samples that drift across plates and raise volcanic chains).
- Rifting separates fragments directly rather than tearing progressively.
- No drainage; compatible with later river network generation and stream power erosion.

## 14. Implementation checklist

- [ ] Fibonacci sampling, spherical Delaunay, per-plate BVH.
- [ ] Data model of §2, with crust-type-dependent fields.
- [ ] Plate rotation about $\mathbf{w}$ by $\omega \, \delta t$.
- [ ] Boundary tracking and distance-to-front overestimation.
- [ ] Subduction polarity rules, Eq. 1–5, slab pull Eq. 6 with renormalization.
- [ ] Collision trigger at 300 km, Eq. 7–11, terrane transfer.
- [ ] Erosion, dampening, trench fill, Eq. 15–17.
- [ ] Periodic resampling with oceanic crust generation, Eq. 12–13.
- [ ] Rifting, Eq. 14.
- [ ] Every **Unspecified** choice recorded as a named parameter.
- [ ] All randomness from seeded streams; fixed update order for determinism.
