# Analytical Terrain Erosion: Implementation Reference

Implementation reference distilled from Tzathas, Gailleton, Steer, Cordonnier, *Physically-based analytical erosion for fast terrain generation*, Computer Graphics Forum 43(2), 2024, DOI [10.1111/cgf.15033](https://doi.org/10.1111/cgf.15033).
The full transcription, with figures and rationale, is [research/2024-Physically-based.md](research/2024-Physically-based.md).
Equation numbers (Eq. n) match that transcription.
Reference code (Python, numba): https://gitlab.inria.fr/landscapes/analytical-terrains.

Conventions in this document:

- **Paper** marks behavior the paper states.
- **Unspecified** marks something the paper leaves open; an implementation must choose, and should record the choice.
- Numerical constants are calibration values from the paper's examples, not contracts.

The method solves the same stream power law as the 2016 paper ([large-scale-terrain-generation.md](large-scale-terrain-generation.md)), but evaluates the elevation at a chosen time $t$ directly instead of time-stepping.

## 1. Problem

| Item | Definition |
| --- | --- |
| Grid | Regular square grid, spacing $\delta x$, 4-neighborhood. |
| Inputs | Initial elevation $z_0$; uplift rate $u$; boundary mask $b$; target time $t$. |
| Boundary | Cells with $b$ = true keep $z = z_0$. At least one is required. Uplift must be 0 there (Eq. 4). |
| Output | Elevation $z$ at time $t$. |
| Time | A parameter, not an iteration count. Large $t$ gives the steady state. |

Modes:

| Mode | Setting |
| --- | --- |
| Mountain range from nothing | Flat or noisy $z_0$, painted $u$, large $t$ (steady state) |
| Erosion post-process | User or procedural $z_0$, $u = 0$, small $t$ (100–300 ky) |
| Mixed | Any $z_0$, any $u$, any $t$ |

## 2. Governing equations

Stream power law (Eq. 1), with $n = 1$:

$$
\frac{\partial z}{\partial t} = u - k A^m \lVert \nabla z \rVert
$$

Along a river path parameterized by $s$ (distance from the boundary, $s = 0$), with $a(s) = k A(s)^m$ (Eq. 3):

$$
\frac{\partial z}{\partial t} = u(s) - a(s) \frac{\partial z}{\partial s}
$$

Assumptions: $A$ does not change with time; $u$ varies in space, not in time.

Boundary and initial conditions (Eq. 4): $u(0) = 0$; $z(0, t) = z_0(0)$; $z(s, 0) = z_0(s)$.

### Analytical solution

Advection time between positions $x$ and $y$ on a path (Eq. 10):

$$
T(x, y) = \int_x^y \frac{ds'}{a(s')}
$$

Elevation difference from uplift between positions $x$ and $y$ (Eq. 17):

$$
S(x, y) = \int_x^y \frac{u(s')}{a(s')} \, ds'
$$

Advection source $D_{x,t}$: the position with $T(D_{x,t}, x) = t$ (Eq. 11); if $T(0, x) \le t$, then $D_{x,t} = 0$.

$$
z(x, t) = z_0(D_{x,t}) + S(D_{x,t}, x)
\tag{8, 16}
$$

When $D_{x,t} = 0$ this is the steady state (Eq. 9): $z(x) = z_0(0) + S(0, x)$.

## 3. Constants (Paper Table 1)

| Symbol | Meaning | Value |
| :---: | --- | ---: |
| $u$ | Uplift | $10^{-3}$ m/y |
| $k$ | Fluvial erosion | $2 \times 10^{-5}$ m$^{1-2m}$/y |
| $p$ | Precipitation | 1 m/y |
| $m$ | Drainage exponent | 0.4 |
| $n$ | Slope exponent | 1 (required) |
| $C$ | Hack's law constant (range 1.4–2) | 1.5 m$^{1-2h}$ |
| $h$ | Hack's law exponent | 0.6 |
| $k_h$ | Hillslope erosion | 0.1 m²/y |
| $k_t$ | Thermal erosion | $10^{-3}$ m/y |
| $s_c$ | Critical slope | 0.57 ($\tan 30°$) |

Other values used in the paper:

| Name | Value |
| --- | --- |
| Example grid | 512² at $\delta x$ = 50 m (25.6 km square); also 1024², 2048² at the same extent |
| Steady state reached | $t \approx$ 1.6–4.6 My |
| Erosion post-process | $t$ = 100–500 ky |
| Multigrid levels | 4–5 |
| Iterations per level | 5–6 |
| Upsampling jitter | uniform in $[-0.25, 0.25]^2$ cells |
| Optimization weights | $\lambda_r = 1/3$, $\lambda_d = 2/3$ |
| Optimization step | $l_r$ = 0.01, about 50 iterations, $z$ remapped to [0, 1] |

**Unspecified:** the precipitation $p$ is listed but appears in no equation.
The paper's threshold "drainage above 2500 m³/y" (§7.1) suggests $A$ is used as discharge $p \times$ area. With $p$ = 1 m/y the numbers are the same; keep $A$ as area unless $p$ varies.

The paper writes $s_c = \operatorname{atan}(30°)$; the intended value is $\tan 30° \approx 0.577$, matching the table.

Performance reference (Paper Table 2, Python + numba, single CPU):

| Grid | Multigrid | Fixed point | Time-stepping simulation | Optimization (worst case) |
| ---: | ---: | ---: | ---: | ---: |
| 512² | 1.79 s | 7.85 s | 55.5 s | 0.94 s |
| 1024² | 8.18 s | 70.9 s | 556 s | 4.10 s |
| 2048² | 33.7 s | 539 s | 4799 s | 16.6 s |

Multigrid cost is close to linear in cell count.

## 4. Data model

```
Grid {
    Rows, Cols int
    Dx         float      // δx, m
    Z0         []float    // initial elevation
    U          []float    // uplift rate; 0 on boundary cells
    B          []bool     // boundary mask
    Rand       []float    // per-cell uniform random value in [0,1), fixed for the run
    // derived per pass
    Receiver   []int      // downstream 4-neighbor; self on boundary cells
    Order      []int      // topological order, boundaries first
    A          []float    // drainage area
    Acoef      []float    // a = k·A^m (plus hillslope and slope correction)
    Z          []float    // result
}
```

## 5. River network

### 5.1 Receivers (Paper §4.3)

For each non-boundary cell $c$:

1. Candidates: 4-neighbors $c'$ with $z(c') < z(c)$.
2. Choose one at random with probability proportional to $z(c) - z(c')$, using the cell's stored random value $\text{Rand}[c]$ (so the choice is repeatable).

Choosing the lowest neighbor instead produces axis-aligned valleys on a regular grid; the paper rejects it.

### 5.2 Depressions

Cells with no lower neighbor that are not boundaries are local minima.
Reroute them by depression breaching (Scott and Dodgson 2021): carve a path from each pit to a lower outlet so every cell drains to a boundary.
**Unspecified:** the breaching algorithm. Any method that gives every cell a receiver chain ending at a boundary works, for example priority-flood from the boundaries, assigning receivers in flood order.

### 5.3 Drainage area

$A_c = \delta x^2 + \sum_{\text{donors } d \text{ of } c} A_d$, accumulated in reverse topological order (leaves to boundaries).

### 5.4 Slope correction

The single-receiver slope $(z(c) - z(c_r))/\delta x$ underestimates $\lVert \nabla z \rVert$. Replace $a$ with:

$$
a(c) = k A_c^m \, \frac{\delta x \, \lVert \nabla z(c) \rVert}{z(c) - z(c_r)}
$$

with $\lVert \nabla z \rVert$ computed downstream: per axis, the drop from $c$ to the lower of its two neighbors on that axis (0 if neither is lower).
It needs an existing elevation; use the current estimate.

## 6. One-dimensional solver (Paper §4.2)

For one path stored as an array (index 0 = boundary, spacing $\delta x$), with $a$, $u$, $z_0$ per cell, $O(N)$:

1. **Find $x_0$.** Accumulate $T(0, x) = T(0, x - \delta x) + \delta x / a(x)$ from $T(0, 0) = 0$ until the first $x_0$ with $T(0, x_0) > t$.
   For $x < x_0$: $D_{x,t} = 0$.
   At $x_0$ (Eq. 12): $D_{x_0,t} = a(0) \, (T(0, x_0) - t)$.
2. **Propagate $D$** for $x > x_0$ (Eq. 15). Starting from $D_{x-\delta x,t}$, walk forward to the last $x_i$ with $T(D_{x-\delta x,t}, x_i) < \delta x / a(x)$; then

   $$
   D_{x,t} = x_i + a(x_i) \left( \frac{\delta x}{a(x)} - T(D_{x-\delta x,t}, x_i) \right)
   $$

3. **Propagate $S$** with $S(0) = 0$ (Eq. 18):

   $$
   S(x) = S(x - \delta x) + \frac{\delta x \, u(x)}{a(x)} - S(D_{x-\delta x,t}, D_{x,t})
   $$

   where $S(D_{x-\delta x,t}, D_{x,t})$ sums $\delta x \, u / a$ over cells between the two positions, with the end cells scaled by the covered fraction.
4. **Elevation:** $z(x) = z_0(D_{x,t}) + S(x)$, with $z_0(D_{x,t})$ linearly interpolated between the enclosing cells.

Partial integrals at non-cell positions treat $1/a$ and $u/a$ as constant within a cell.

## 7. Two-dimensional solver (Paper §4.3)

Apply §6 on each river tree:

- Receivers and drainage as in §5; boundaries are tree roots.
- Traverse each tree depth-first from the root, keeping the stack of cells from root to current cell; that stack is the 1D array for the current cell.
- The current cell's receiver plays the role of $x - \delta x$; its $D$ and $S$ are already known.
- Look up $D_{x,t}$ positions and partial integrals on the stack.

Complexity is linear per path. Branches share their downstream stack, so the total is linear in practice; **Unspecified:** worst-case bound on trees.

The network depends on the elevations and the elevations on the network; §8 resolves this.

## 8. Coupling network and elevation (Paper §5)

### 8.1 Fixed point (baseline)

```
z = z0
repeat:
    receivers, order, A, slope correction from z
    z_new = analytical solution (§7)
    z = EMA(z, z_new)          // damping against oscillation
until z converges
```

Converges, but basin boundaries move one cell per iteration, so iterations scale with the longest river.
**Unspecified:** EMA weight and convergence threshold.

### 8.2 Multigrid (Paper §5.2)

```
levels L (4–5); build pyramid by 2×2 averaging of z0, u, b
z = solve fixed point at coarsest level for k iterations (5–6)
for each finer level:
    z = bilinear upsample of z, sample positions jittered by U[-0.25, 0.25]^2 cells
    k iterations of fixed point at this level, starting from z
```

The jitter prevents block-aligned rivers and sharp turns.
**Unspecified:** downsampling the boolean mask $b$ (any-true is a safe choice); whether $z_0$ at each level is the downsampled original or the upsampled estimate (the advection source must be the original $z_0$; the upsampled $z$ only seeds the network).
More iterations remove remaining artifacts, or follow with §8.3.

### 8.3 Optimization on a fixed network (Paper §5.3)

Use when the river network must stay as given (preserve input drainage, or a procedural river network).

- Variables: per-cell drops $d_c = z(c) - z(c_r) \ge 0$. Elevation is reconstructed by summing drops from the boundary.
- Targets: $\tilde{d}_c$ = drop from the analytical solution on the fixed network.
- $\mathrm{NCN}(c)$: 4-neighbors $c_n$ where neither is the other's receiver.
- Discontinuity (Eq. 19): $z(c) - z(c_n) > z(c) - z(c_r)$.

Objective (Eq. 20–22):

$$
\min_{\mathbf{d} \ge 0} \; \lambda_r \sum_c \frac{(d_c - \tilde{d}_c)^2}{2} + \lambda_d \sum_c \sum_{c_n \in \mathrm{NCN}(c)} \frac{\max(z_c - z_{c_n} - \tilde{d}_c, 0)^2}{2}
$$

Gradients (Eq. 30, 32), the second accumulated from donors like drainage:

$$
\frac{\partial L_r}{\partial d_i} = d_i - \tilde{d}_i
$$

$$
\frac{\partial L_d}{\partial d_i} = \sum_{j \in \text{donors}(i)} \frac{\partial L_d}{\partial d_j} + \sum_{i_n \in \mathrm{NCN}(i)} \Bigl( \max(z_i - z_{i_n} - \tilde{d}_i, 0) - \max(z_{i_n} - z_i - \tilde{d}_{i_n}, 0) \Bigr)
$$

Procedure:

1. Remap $z$ to [0, 1].
2. Gradient descent, step $l_r$ = 0.01, about 50 iterations.
3. Scale $\partial L_d / \partial d_c$ by the upstream cell count $N_c = A_c / \delta x^2$ against exploding gradients.
   The paper says "weigh by"; dividing is the reading that prevents explosion. **Unspecified.**
4. Enforce $d_c \ge 0$ by shrinking the local step. **Unspecified:** exact rule; clamping at 0 is the simplest.
5. Rebuild $z$ from $\mathbf{d}$ each iteration.

Multigrid then optimization gives a continuum from geologically accurate (multigrid) to network-preserving (optimization alone).

## 9. Additional erosion processes (Paper §6)

Pure stream power makes infinitely steep ridges where $A \to 0$. Both additions below only change $u$ and $a$, so §6 still applies.

### 9.1 Hillslope

Steady-state equivalent of linear diffusion $\partial z / \partial t = k_h \Delta z$, using Hack's law $r - s = C A^h$ (distance to ridge):

$$
a(s) = k A(s)^m + \frac{k_h}{C} A(s)^{-h}
\tag{26}
$$

Exact at steady state near ridges; an approximation in transients.

### 9.2 Thermal (landslides)

Above the critical slope $s_c$ (Eq. 27–29):

$$
u'(s) = u(s) + k_t s_c, \qquad a'(s) = k A^m + \frac{k_h}{C} A^{-h} + k_t \qquad \text{if } \frac{\partial z}{\partial s} > s_c
$$

Otherwise $u' = u$, $a' = k A^m + \frac{k_h}{C} A^{-h}$.

Implementation: in §6, interleave steps 2–4 per cell. Compute $z(x)$ without the thermal term; if the slope to the receiver exceeds $s_c$, recompute $D_{x,t}$, $S(x)$, $z(x)$ with $u'$, $a'$.

## 10. Variants

| Variant | Change |
| --- | --- |
| Hydrology-guided slopes (Génevaux et al. 2013 comparison) | Choose $a$ to force $\lVert \nabla z \rVert$ = 0.06 where drainage > 2500 m³/y and 0.6 elsewhere |
| Sloped boundary (canyons) | Extend $D_{x,t}$ below 0 with constant $a = a(0)$; take $z_0$ beyond the boundary from a constant slope. Gives V-shaped canyon walls instead of flat floors. |
| Imposed river source | Add a large drainage area at a chosen cell to force a main river from it |
| Escarpment retreat | $u = 0$, $z_0$ with a cliff; the cliff retreats along drainage over time |

## 11. Known limitations (from the paper)

- Not continuous in $t$: changing $t$ can move valleys, especially at large $t$; multigrid can worsen this.
- Requires $n = 1$.
- No sediment deposition; no other erosion laws without new derivations.
- Tree traversals are sequential; not trivially parallel.

## 12. Implementation checklist

- [ ] Grid inputs $z_0$, $u$ (0 on boundaries), $b$, $t$, and per-cell stored random values.
- [ ] Weighted-random 4-neighbor receivers; depression breaching to boundaries.
- [ ] Drainage accumulation in topological order.
- [ ] Slope-corrected $a$, with optional hillslope term.
- [ ] 1D solver: $x_0$, $D_{x,t}$, $S(x)$, interpolated $z_0(D_{x,t})$.
- [ ] Depth-first tree traversal with the root-to-cell stack.
- [ ] Thermal re-evaluation per cell.
- [ ] Fixed point with damping; multigrid pyramid with jittered upsampling.
- [ ] Optional optimization on a fixed network (Eq. 20–22, 30, 32).
- [ ] Every **Unspecified** choice recorded as a named parameter.
- [ ] All randomness (receivers, jitter, noise) from seeded streams.
