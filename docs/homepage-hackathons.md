# Homepage hackathon gallery

The homepage uses real bitcoin++ event photos. AVIF copies are in `static/img/home/`; each gallery photo links to its event’s hackathon page (`/berlin25/hackathon`, `/nairobi/hackathon`, or `/berlin26/hackathon`). The gallery scrolls horizontally on mobile; CSS crops only the thumbnails.

| Asset | Source |
| --- | --- |
| `berlin25-hackathon-awards.avif` | 2027 bitcoin++ sponsor deck, slide 6: Glacier Wallet receiving its Berlin 2025 hackathon award. |
| `nairobi26-hackathon-awards.avif` | Presentation research asset `outputs/bitcoin-plus-plus-showcase/dada-devs-nairobi-powr-stage.jpg`: PoWR team at the Nairobi 2026 hackathon awards. |
| `berlin26-hackathon-demo.avif` | Presentation research asset `outputs/bitcoin-plus-plus-showcase/berlin26-bark-winner-1.jpg`: Barkade demo at the Berlin 2026 hackathon. |

## Prize statistic

The existing deck research snapshot (`outputs/bitcoin-plus-plus-showcase/prize-awards.json`) records **129,895,000 sats awarded** across ten events. The homepage rounds down to **129M+ sats awarded**, a historical lower bound rather than a live counter. These are configured sats prizes attached to finalized project/prize assignments in public results, not payment settlement records or unassigned prize pools. Do not describe the number as sats paid out.

| Event | Sats awarded | Public results source |
| --- | ---: | --- |
| Berlin 2026 | 21,750,000 | [Results](https://btcpp.dev/api/v1/hackathons/3e888537-1021-4d90-8a01-7f7b5208f2ad/results) |
| Berlin 2024 | 22,000,000 | [Results](https://btcpp.dev/api/v1/hackathons/d1547c87-fc6a-4e9b-a51c-433b2b9478eb/results) |
| Toronto | 7,042,000 | [Results](https://btcpp.dev/api/v1/hackathons/c27907f8-f81c-46fb-9910-4d67e7d4c54c/results) |
| Nairobi | 9,420,000 | [Results](https://btcpp.dev/api/v1/hackathons/056e9d27-7fc2-4668-a04d-22ac4330cd07/results) |
| Istanbul | 7,000,000 | [Results](https://btcpp.dev/api/v1/hackathons/bf8a9014-65d0-421a-bad9-9dba0685527d/results) |
| Riga | 5,000,000 | [Results](https://btcpp.dev/api/v1/hackathons/5caf84f1-8848-4688-8ab0-ebebc51ee089/results) |
| Taipei | 8,992,000 | [Results](https://btcpp.dev/api/v1/hackathons/af9bfa9a-0efa-41c1-8bf8-3ca9d881d878/results) |
| Berlin 2025 | 11,592,000 | [Results](https://btcpp.dev/api/v1/hackathons/b2caf31c-035e-44f3-b5fc-1554f6762cef/results) |
| Florianópolis | 27,099,000 | [Results](https://btcpp.dev/api/v1/hackathons/2199a87c-9310-4a50-89d5-0c8bcdee6d42/results) |
| Austin 2025 | 10,000,000 | [Results](https://btcpp.dev/api/v1/hackathons/65a48b0a-6b92-4c01-ac2e-69f72ee6cf0a/results) |

When refreshing this figure, recompute from finalized public assignments, deduplicate each project/prize assignment, and keep the label and rounding consistent with the source.
