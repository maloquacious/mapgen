# Large Scale Terrain Generation: Implementation Reference

Implementation reference distilled from Cordonnier, Braun, Cani, Benes, Galin, Peytavie, Guérin, *Large Scale Terrain Generation from Tectonic Uplift and Fluvial Erosion*, Computer Graphics Forum 35(2), 2016, pp. 165–175, DOI [10.1111/cgf.12820](https://doi.org/10.1111/cgf.12820).
The full transcription, with figures and rationale, is [research/2016-Large-Scale-Terrain.md](research/2016-Large-Scale-Terrain.md).
Equation numbers (Eq. n) match that transcription; unnumbered equations there are labeled here by name.

Conventions in this document:

- **Paper** marks behavior the paper states.
- **Unspecified** marks something the paper leaves open; an implementation must choose, and should record the choice.
- Numerical constants are calibration values from the paper's examples, not contracts.

## 1. Domain

| Item | Definition |
| --- | --- |
| Domain | $\Omega \subset \mathbb{R}^2$, planar region where the uplift map $\mathcal{U} \neq 0$. |
| Input | Uplift map $\mathcal{U} : \Omega \to \mathbb{R}_{\ge 0}$, piecewise uniform, given as a gray-scale image (uplift rate, m/y). |
| Output | Stream graph $\mathcal{G}$ with per-node elevation and per-node receiver (flow direction); then an elevation function $h : \Omega \to \mathbb{R}$. |
| Boundary | Nodes on $\partial\Omega$ are outflows (river mouths) at fixed elevation (sea level). |
| Distances | Euclidean, horizontal plane. |

## 2. Governing equation

Stream power law (Eq. 1):

$$
\frac{dh(\mathbf{p})}{dt} = u(\mathbf{p}) - k \, A(\mathbf{p})^m \, s(\mathbf{p})^n
$$

| Symbol | Meaning | Units |
| :---: | --- | --- |
| $h$ | Elevation | m |
| $u$ | Uplift rate, $\mathcal{U}(\mathbf{p})$ | m/y |
| $k$ | Erodibility (lithology, climate, vegetation) | y⁻¹ for $m = 0.5$, $n = 1$ |
| $A$ | Drainage area: total area upstream of $\mathbf{p}$ | m² |
| $s$ | Slope toward the receiver | dimensionless |
| $m$, $n$ | Exponents; $m/n$ controls river profile shape | |

The implicit solver below requires $n = 1$.

## 3. Constants

| Symbol | Meaning | Value |
| :---: | --- | ---: |
| $m$ | Drainage area exponent | 0.5 |
| $n$ | Slope exponent | 1 |
| $\max \mathcal{U}$ | Maximum uplift | $5.0 \times 10^{-4}$ m/y |
| $k$ | Erosion rate | $5.61 \times 10^{-7}$ y⁻¹ |
| $\delta t$ | Time step | $2.5 \times 10^5$ y |
| $\theta_{\max}$ | Thermal erosion maximum slope (talus angle) | 30° |
| $\theta$ range | Noise-modulated talus angle | 6° to 54° |
| Domain size | Example domain | 50 km × 50 km |
| Iterations | Mountains formed / converged | ~50 / 100–300 |
| Convergence threshold | Max $\lvert \Delta h \rvert$ per step | **Unspecified** |

Height calibration (Paper §6.4, empirical, linear):

$$
h_{\max} = 2.244 \, u / k
$$

With the constants above, $u/k \approx 891$ m and $h_{\max} \approx 2000$ m, the paper's stated target.
So $h_{\max}$ comes out in meters when $u$ is in m/y and $k$ in y⁻¹; the paper's text says "kilometers", which does not match its own numbers.
The factor 2.244 was measured on the paper's 50 km domain and sampling; **Unspecified** whether it holds at other scales. Recalibrate.

Only the ratio $u/k$ matters for final relief (both are multiplied by $\delta t$).

Performance reference (Paper Table 1):

| Samples | One step (s) | Convergence (s) |
| ---: | ---: | ---: |
| 10 000 | 0.031 | 6.4 |
| 40 000 | 0.177 | 35.4 |
| 90 000 | 0.401 | 78.0 |
| 160 000 | 1.273 | 252.0 |

Iteration count to converge does not depend on resolution (implicit scheme).

## 4. Data model

### Node $\mathcal{N}_k$

```
Node {
    P         vec2     // position p_k
    H         float    // elevation h_k, initially 0
    U         float    // uplift u_k = U(p_k)
    K         float    // erodibility k (may vary per node)
    M         float    // exponent m (may vary per node); n = 1
    Area      float    // a_k, area of the node's Voronoi cell
    External  bool     // on the domain border: fixed H, outflow
    MaxSlope  float    // thermal talus angle θ_k (constant or noise-modulated)

    // rebuilt every iteration
    Receiver  int      // index of downstream node; self if root
    Children  []int    // donors: nodes whose Receiver is this node
    Drainage  float    // A_k
    Lake      int      // L(N_k): index of the root of this node's stream tree
}
```

### Graph $\mathcal{G}$

- Nodes: Poisson-disk samples of $\Omega$ (guaranteed minimum distance, full coverage).
- Edges: constrained Delaunay triangulation of the nodes, constrained to the border of $\Omega$. Undirected; fixed for the whole run.
- Quasi-uniform edge length sets the feature scale.
- A regular grid also works but produces less natural streams.

### Lake graph $G_L$ (rebuilt every iteration)

```
LakeNode { Root int; External bool }               // one per stream tree
LakeArc  { From, To int; PassX, PassY int; PassH float }
// PassX in lake From, PassY in lake To; PassH = max(h[PassX], h[PassY])
```

## 5. Initialization

1. Poisson-disk sample $\Omega$; build the constrained Delaunay triangulation.
2. Mark nodes on $\partial\Omega$ as External; their $h$ is fixed at 0 (sea level).
3. For every node: $h_k = 0$, $u_k = \mathcal{U}(\mathbf{p}_k)$, $k$, $m$ from parameters.
4. Compute the Voronoi tessellation clipped to $\Omega$; $a_k$ = cell area.

## 6. Iteration

```
repeat:
    build stream trees            // §6.1
    connect lakes                 // §6.2
    compute drainage area         // §6.3
    solve stream power (implicit) // §6.4
    apply thermal slope limit     // §6.5
until converged                   // §6.6
```

### 6.1 Stream trees $\mathcal{T}$

For each non-External node $\mathcal{N}_k$:

- Receiver = neighbor with lowest $h$, if that $h < h_k$ (steepest descent by elevation, Paper: "neighbor of the lowest elevation").
- Otherwise Receiver = self (a local minimum, i.e. a lake root).

External nodes are always roots.
Receivers never form cycles, so $\mathcal{T}$ is a forest; roots are lakes (interior) or outflows (External).
First iteration: all $h = 0$, so every node is its own root.

**Unspecified:** tie-breaking between equal neighbors; use a fixed rule (for example, lowest index) for determinism.
**Unspecified:** whether "lowest" is by elevation or by slope (elevation difference over distance); the paper says elevation.

### 6.2 Lake overflow (Paper §4.3), $O(N + M \log M)$

Turns the forest into trees that all drain to External roots, producing $\widetilde{\mathcal{T}}$.

1. **Label.** For each tree, set $L(\mathcal{N}) = $ root index for every node in it.
2. **Lake graph.** One $G_L$ node per tree; External roots give External lake nodes.
3. **Passes.** For each pair of lakes $(l_0, l_1)$ sharing at least one graph edge $(\mathcal{X}, \mathcal{Y})$ with $L(\mathcal{X}) = l_0$, $L(\mathcal{Y}) = l_1$, the pass is the edge minimizing $\max(h_\mathcal{X}, h_\mathcal{Y})$ (the pass height).
   Add arcs in both directions $l_0 \to l_1$ and $l_1 \to l_0$, carrying the pass.
   One $O(N)$ sweep over the edges, keeping the minimum per lake pair.
4. **Remove** all arcs leaving External lake nodes.
5. **Grow trees $T_L$** (a minimum-spanning-tree-like sweep):

   ```
   inTree = {External lake nodes}
   candidates = ordered set of arcs (i -> j) with i ∉ inTree, j ∈ inTree, keyed by PassH
   while candidates not empty:
       e = pop lowest PassH
       if e.From ∈ inTree: discard; continue
       add e to T_L; inTree += e.From
       for each arc (k -> e.From) with k ∉ inTree: add to candidates
   ```

   The paper uses a red-black tree; any ordered set or min-heap works.
   Never add an arc whose source is already in the tree.
   **Unspecified:** tie-breaking on equal PassH.
   **Unspecified:** lakes with no path to any External node (disconnected domain); none exist if $\Omega$ is connected.

6. **Merge into $\widetilde{\mathcal{T}}$.** For each arc $e(i, j) \in T_L$ with pass $(\mathcal{X}_i, \mathcal{X}_j)$, $\mathcal{X}_j$ on lake $j$'s side: set Receiver of the lake bottom $\mathcal{N}_i$ (root of lake $i$) to $\mathcal{X}_j$.
   All water reaching lake $i$ then flows into lake $j$ through $\mathcal{X}_j$.

   The paper's text says "add an arc $e(\mathcal{X}_i, \mathcal{X}_j)$" while its Figure 7 connects $\mathcal{N}_i$ to $\mathcal{X}_j$.
   Only the Figure 7 form keeps every node with a single receiver; use it.

After this step every node drains to an External root.

### 6.3 Drainage area and slope

$$
A_k = a_k + \sum_{\mathcal{N}_l \in C(\mathcal{N}_k)} A_l
$$

where $C(\mathcal{N}_k)$ = nodes whose Receiver is $\mathcal{N}_k$ in $\widetilde{\mathcal{T}}$.

Algorithm, $O(N)$:

1. Breadth-first traversal from all External roots down through Children; record visit order in list $P$.
2. Set $A_k = a_k$; iterate $P$ in reverse, adding each node's $A$ to its Receiver's $A$.

Slope from node $i$ to receiver $j$:

$$
s_i = \frac{h_i - h_j}{\lVert \mathbf{p}_i - \mathbf{p}_j \rVert}
$$

### 6.4 Implicit stream power solve (Eq. 2)

For node $i$ with receiver $j$, $d_{ij} = \lVert \mathbf{p}_i - \mathbf{p}_j \rVert$, $n = 1$:

$$
h_i(t + \delta t) = \frac{h_i(t) + \delta t \left( u_i + \dfrac{k A_i^m}{d_{ij}} \, h_j(t + \delta t) \right)}{1 + \dfrac{k A_i^m}{d_{ij}} \, \delta t}
$$

- Process nodes in the order $P$ (roots to leaves) so $h_j(t + \delta t)$ is known before $h_i$.
- External nodes are not updated.
- $O(N)$ per iteration; unconditionally stable, allowing large $\delta t$.

**Unspecified:** handling a lake bottom $\mathcal{N}_i$ whose new receiver (the pass node) is higher than it.
Eq. 2 then pulls $h_i$ up toward $h_j$, filling the lake. That matches the intended behavior; nothing in the paper prevents it.

### 6.5 Thermal erosion correction

After Eq. 2, limit slopes to the talus angle $\theta$:

$$
h_i \leftarrow \min\left( h_i, \; h_j + \tan(\theta_i) \, d_{ij} \right)
$$

Paper: "if the result of the stream power equation leads to slopes higher than 30°, we reduce the change of elevation so as to keep slopes in the prescribed range."
**Unspecified:** whether the limit applies to the receiver only (as above) or to all neighbors, and whether it is applied inside the root-to-leaf sweep (so later nodes see limited values) or after it.
Applying it inside the sweep, toward the receiver, is the cheapest consistent choice.

Variants used in the paper:

| Variant | Rule |
| --- | --- |
| Uniform | $\theta = 30°$ |
| Rock strength variation | $\theta(\mathbf{p})$ from high-persistence 3D Perlin noise, mapped to [6°, 54°] |
| Cliffed crests | $\theta$ small below a height threshold, larger above it |

### 6.6 Convergence

Stop when $\max_k \lvert h_k(t + \delta t) - h_k(t) \rvert$ < threshold (**Unspecified** value).
Paper notes: changes in stream-tree topology (receivers) are an accurate convergence predictor; typical convergence is 100–300 iterations.

## 7. Erosion without uplift

Set $u = 0$ and initialize $h$ from an existing heightfield (for example fractal noise) to impose drainage on it.
The surface then decays toward flat: use small $\delta t$ and stop after a few iterations.
**Unspecified:** step size and count.

## 8. River profile choice ($m/n$)

At steady state ($dh/dt = 0$) with uniform $u$, $k$: $s \sim A^{-m/n}$.
Assume $A \sim (d - x)^2$, with $x$ the distance from the outflow and $d$ the outflow-to-ridge distance; integrate with $h(0) = 0$:

$$
h(x) \sim
\begin{cases}
d^{1-2m/n} - (d - x)^{1-2m/n} & m/n < 1/2 \\
\log d - \log(d - x) & m/n = 1/2 \\
(d - x)^{1-2m/n} - d^{1-2m/n} & m/n > 1/2
\end{cases}
$$

Expose $m$ (with $n = 1$) as the profile-shape control.

## 9. Uplift map effects (Paper §6.4)

| Uplift input | Result |
| --- | --- |
| Smooth, slowly decreasing gradient | Straight valleys aligned with the gradient |
| Stepped constant regions | More random valleys, abrupt height jumps |
| Lower uplift | Smaller valleys |
| Higher uplift | Thermal limit dominates; regular slopes |

The gradient of $\mathcal{U}$ matters more than its exact shape, except at the domain boundary.

## 10. Elevation function from the graph

| Method | Definition | Use |
| --- | --- | --- |
| Mesh | Triangulate nodes with Phong tessellation | Interactive preview |
| Kernel blend | $h(\mathbf{p}) = \dfrac{\sum_k G_\sigma(\lVert \mathbf{p} - \mathbf{p}_k \rVert) \, h_k}{\sum_k G_\sigma(\lVert \mathbf{p} - \mathbf{p}_k \rVert)}$, Gaussian $G_\sigma$ | Smoother preview; **Unspecified** $\sigma$ (about the node spacing) |
| Feature primitives | Convert to a construction tree of ridge, valley, and river primitives (Génevaux et al. 2015; Guérin et al. 2016), rivers carved from $h$ and $A$ | High detail, off-line |

Lakes for display: a node belongs to a lake if it drains into lake root $\mathcal{N}_i$ and its $h$ is below that lake's pass height.

## 11. Known limitations (from the paper)

- No sediment deposition (relevant on low slopes).
- Fluvial erosion only: no glacial, hillslope (beyond the thermal limit), or alluvial processes.
- Parameters $k$, $m$, $n$ are poorly constrained geologically.
- Not parallelized.
- Suggested extension: derive the uplift map from a plate-tectonics simulation (see [procedural-planetary-tectonics.md](procedural-planetary-tectonics.md)).

## 12. Implementation checklist

- [ ] Poisson-disk sampling, constrained Delaunay, clipped Voronoi cell areas.
- [ ] External border nodes with fixed elevation.
- [ ] Receivers by lowest neighbor, deterministic ties.
- [ ] Lake labeling, pass search, ordered candidate sweep, lake-bottom-to-pass receivers.
- [ ] BFS order $P$; drainage area in reverse order.
- [ ] Implicit Eq. 2 in order $P$, External nodes fixed.
- [ ] Thermal slope limit, with optional per-node $\theta$.
- [ ] Convergence on max $\lvert \Delta h \rvert$ and receiver changes.
- [ ] $h_{\max} = 2.244\,u/k$ recalibrated for the actual domain and resolution.
- [ ] Every **Unspecified** choice recorded as a named parameter.
- [ ] All randomness (sampling, noise) from seeded streams.
