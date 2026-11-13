# Three-run commit and ablation comparison

All LCP values are milliseconds. The main and original PR builds use identical seeded data, profile and settled probe; the diagnostic override changes only the added fades/view-transition rule. Final runtime source: `58c9a79`. Full shift sources and LCP candidate elements are in the JSON files.

| Variant | Page | LCP samples | CLS samples | LCP median |
| --- | --- | --- | --- | ---: |
| before | / | 3064, 3064, 3344 | 0.071903, 0.071903, 0.071903 | 3064 |
| before | /events | 3048, 2992, 2936 | 0.000488, 0.000488, 0.000488 | 2992 |
| before | /members | 3868, 2600, 2588 | 0.000488, 0.000488, 0.000488 | 2600 |
| before | /finance | 932, 948, 948 | 0.000488, 0.000488, 0.000488 | 948 |
| before | /design | 3072, 3004, 3000 | 0.000488, 0.000488, 0.000488 | 3004 |
| before | /officers | 1008, 988, 980 | 0.000034, 0.000488, 0.000488 | 988 |
| original-pr | / | 1860, 1888, 1880 | 0.071903, 0.071903, 0.071903 | 1880 |
| original-pr | /events | 1828, 1812, 1952 | 0.000488, 0.000488, 0.000488 | 1828 |
| original-pr | /members | 1632, 1616, 1776 | 0.000488, 0.000488, 0.000488 | 1632 |
| original-pr | /finance | 508, 496, 636 | 0.000488, 0.000488, 0.000488 | 508 |
| original-pr | /design | 504, 512, 628 | 0.000488, 0.000488, 0.000488 | 512 |
| original-pr | /officers | 504, 504, 560 | 0.000488, 0.000488, 0.000488 | 504 |
| without-added-fades | / | 2184, 2768, 1860 | 0.071781, 0.071903, 0.071903 | 2184 |
| without-added-fades | /events | 3288, 2216, 1796 | 0.000488, 0.000488, 0.000488 | 2216 |
| without-added-fades | /members | 2208, 2436, 1616 | 0.000488, 0.000488, 0.000488 | 2208 |
| without-added-fades | /finance | 512, 536, 496 | 0.000488, 0.000488, 0.000488 | 512 |
| without-added-fades | /design | 2648, 2016, 1840 | 0.000488, 0.000488, 0.000488 | 2016 |
| without-added-fades | /officers | 644, 528, 536 | 0.000488, 0.000488, 0.000488 | 536 |
| paintable-before-members-fix | / | 1820, 1800, 1792 | 0.000000, 0.000000, 0.000000 | 1800 |
| paintable-before-members-fix | /events | 1832, 1832, 1828 | 0.000000, 0.000000, 0.000000 | 1832 |
| paintable-before-members-fix | /members | 1616, 1624, 1608 | 0.015537, 0.015537, 0.015537 | 1616 |
| paintable-before-members-fix | /finance | 504, 508, 496 | 0.000000, 0.000000, 0.000000 | 504 |
| paintable-before-members-fix | /design | 1880, 1820, 1840 | 0.000000, 0.000000, 0.000000 | 1840 |
| paintable-before-members-fix | /officers | 500, 504, 500 | 0.000000, 0.000000, 0.000000 | 500 |
| after | / | 1800, 1792, 1808 | 0.000000, 0.000000, 0.000000 | 1800 |
| after | /events | 1844, 1800, 1808 | 0.000000, 0.000000, 0.000000 | 1808 |
| after | /members | 1572, 1616, 1572 | 0.000000, 0.000000, 0.000000 | 1572 |
| after | /finance | 488, 500, 488 | 0.000000, 0.000000, 0.000000 | 488 |
| after | /design | 1840, 1832, 1832 | 0.000000, 0.000000, 0.000000 | 1832 |
| after | /officers | 500, 504, 520 | 0.000000, 0.000000, 0.000000 | 504 |
