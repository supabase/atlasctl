package selection

// Zone identifies a broad geographic region used for intra-round probe interleaving.
// The six zones roughly correspond to the world's major IP-infrastructure regions.
type Zone int

const (
	ZoneNA    Zone = iota // North America (US, CA)
	ZoneEU                // Europe (including Russia and Caucasus)
	ZoneAPAC              // Asia-Pacific (East/South/Southeast Asia, Oceania)
	ZoneLATAM             // Latin America (MX, Central America, South America, Caribbean)
	ZoneMENA              // Middle East + North Africa
	ZoneSSA               // Sub-Saharan Africa
)

// zoneOrder defines the fixed rotation for the continental interleaving pass.
// Within each band tier, zones are visited in this order before any zone gets a
// second pick. The order is arbitrary but fixed for determinism.
var zoneOrder = []Zone{ZoneNA, ZoneEU, ZoneAPAC, ZoneLATAM, ZoneMENA, ZoneSSA}

// ZoneOf returns the Zone for a given ISO 3166-1 alpha-2 country code.
// Unknown or empty codes default to ZoneNA (negligible in practice — every
// connected RIPE Atlas probe carries a valid country code).
func ZoneOf(cc string) Zone {
	if z, ok := countryZone[cc]; ok {
		return z
	}
	return ZoneNA
}

// countryZone maps ISO 3166-1 alpha-2 country codes to Zone constants.
// Coverage targets every country that appears in the RIPE Atlas probe network.
var countryZone = map[string]Zone{
	// ── North America ──────────────────────────────────────────────────────────
	"CA": ZoneNA,
	"US": ZoneNA,
	"PR": ZoneNA, // Puerto Rico (US territory)
	"GU": ZoneNA, // Guam (US territory, but grouped with NA over APAC by convention)
	"VI": ZoneNA, // US Virgin Islands

	// ── Europe ─────────────────────────────────────────────────────────────────
	"AD": ZoneEU, // Andorra
	"AL": ZoneEU, // Albania
	"AM": ZoneEU, // Armenia (Caucasus, EU-facing network)
	"AT": ZoneEU, // Austria
	"AZ": ZoneEU, // Azerbaijan (Caucasus)
	"BA": ZoneEU, // Bosnia and Herzegovina
	"BE": ZoneEU, // Belgium
	"BG": ZoneEU, // Bulgaria
	"BY": ZoneEU, // Belarus
	"CH": ZoneEU, // Switzerland
	"CY": ZoneEU, // Cyprus
	"CZ": ZoneEU, // Czech Republic
	"DE": ZoneEU, // Germany
	"DK": ZoneEU, // Denmark
	"EE": ZoneEU, // Estonia
	"ES": ZoneEU, // Spain
	"FI": ZoneEU, // Finland
	"FO": ZoneEU, // Faroe Islands
	"FR": ZoneEU, // France
	"GB": ZoneEU, // United Kingdom
	"GE": ZoneEU, // Georgia (Caucasus, EU-facing)
	"GI": ZoneEU, // Gibraltar
	"GR": ZoneEU, // Greece
	"HR": ZoneEU, // Croatia
	"HU": ZoneEU, // Hungary
	"IE": ZoneEU, // Ireland
	"IM": ZoneEU, // Isle of Man
	"IS": ZoneEU, // Iceland
	"IT": ZoneEU, // Italy
	"JE": ZoneEU, // Jersey
	"LI": ZoneEU, // Liechtenstein
	"LT": ZoneEU, // Lithuania
	"LU": ZoneEU, // Luxembourg
	"LV": ZoneEU, // Latvia
	"MC": ZoneEU, // Monaco
	"MD": ZoneEU, // Moldova
	"ME": ZoneEU, // Montenegro
	"MK": ZoneEU, // North Macedonia
	"MT": ZoneEU, // Malta
	"NL": ZoneEU, // Netherlands
	"NO": ZoneEU, // Norway
	"PL": ZoneEU, // Poland
	"PT": ZoneEU, // Portugal
	"RO": ZoneEU, // Romania
	"RS": ZoneEU, // Serbia
	"RU": ZoneEU, // Russia
	"SE": ZoneEU, // Sweden
	"SI": ZoneEU, // Slovenia
	"SK": ZoneEU, // Slovakia
	"SM": ZoneEU, // San Marino
	"TR": ZoneEU, // Turkey (geographically bridging; EU for network purposes)
	"UA": ZoneEU, // Ukraine
	"VA": ZoneEU, // Vatican City
	"XK": ZoneEU, // Kosovo (not ISO-official, used by RIPE)

	// ── Asia-Pacific ───────────────────────────────────────────────────────────
	"AF": ZoneAPAC, // Afghanistan
	"AU": ZoneAPAC, // Australia
	"BD": ZoneAPAC, // Bangladesh
	"BN": ZoneAPAC, // Brunei
	"BT": ZoneAPAC, // Bhutan
	"CC": ZoneAPAC, // Cocos (Keeling) Islands
	"CK": ZoneAPAC, // Cook Islands
	"CN": ZoneAPAC, // China
	"CX": ZoneAPAC, // Christmas Island
	"FJ": ZoneAPAC, // Fiji
	"FM": ZoneAPAC, // Micronesia
	"HK": ZoneAPAC, // Hong Kong
	"ID": ZoneAPAC, // Indonesia
	"IN": ZoneAPAC, // India
	"IO": ZoneAPAC, // British Indian Ocean Territory
	"JP": ZoneAPAC, // Japan
	"KG": ZoneAPAC, // Kyrgyzstan
	"KH": ZoneAPAC, // Cambodia
	"KI": ZoneAPAC, // Kiribati
	"KP": ZoneAPAC, // North Korea
	"KR": ZoneAPAC, // South Korea
	"KZ": ZoneAPAC, // Kazakhstan
	"LA": ZoneAPAC, // Laos
	"LK": ZoneAPAC, // Sri Lanka
	"MH": ZoneAPAC, // Marshall Islands
	"MM": ZoneAPAC, // Myanmar
	"MN": ZoneAPAC, // Mongolia
	"MO": ZoneAPAC, // Macao
	"MV": ZoneAPAC, // Maldives
	"MY": ZoneAPAC, // Malaysia
	"NC": ZoneAPAC, // New Caledonia
	"NP": ZoneAPAC, // Nepal
	"NR": ZoneAPAC, // Nauru
	"NU": ZoneAPAC, // Niue
	"NZ": ZoneAPAC, // New Zealand
	"PF": ZoneAPAC, // French Polynesia
	"PG": ZoneAPAC, // Papua New Guinea
	"PH": ZoneAPAC, // Philippines
	"PK": ZoneAPAC, // Pakistan
	"PN": ZoneAPAC, // Pitcairn
	"PW": ZoneAPAC, // Palau
	"SB": ZoneAPAC, // Solomon Islands
	"SG": ZoneAPAC, // Singapore
	"TH": ZoneAPAC, // Thailand
	"TJ": ZoneAPAC, // Tajikistan
	"TK": ZoneAPAC, // Tokelau
	"TL": ZoneAPAC, // Timor-Leste
	"TM": ZoneAPAC, // Turkmenistan
	"TO": ZoneAPAC, // Tonga
	"TV": ZoneAPAC, // Tuvalu
	"TW": ZoneAPAC, // Taiwan
	"UZ": ZoneAPAC, // Uzbekistan
	"VN": ZoneAPAC, // Vietnam
	"VU": ZoneAPAC, // Vanuatu
	"WF": ZoneAPAC, // Wallis and Futuna
	"WS": ZoneAPAC, // Samoa

	// ── Latin America ──────────────────────────────────────────────────────────
	"AG": ZoneLATAM, // Antigua and Barbuda
	"AI": ZoneLATAM, // Anguilla
	"AN": ZoneLATAM, // Netherlands Antilles (obsolete code, still seen in some data)
	"AR": ZoneLATAM, // Argentina
	"AW": ZoneLATAM, // Aruba
	"BB": ZoneLATAM, // Barbados
	"BL": ZoneLATAM, // Saint Barthélemy
	"BO": ZoneLATAM, // Bolivia
	"BQ": ZoneLATAM, // Caribbean Netherlands
	"BR": ZoneLATAM, // Brazil
	"BS": ZoneLATAM, // Bahamas
	"BZ": ZoneLATAM, // Belize
	"CL": ZoneLATAM, // Chile
	"CO": ZoneLATAM, // Colombia
	"CR": ZoneLATAM, // Costa Rica
	"CU": ZoneLATAM, // Cuba
	"CW": ZoneLATAM, // Curaçao
	"DM": ZoneLATAM, // Dominica
	"DO": ZoneLATAM, // Dominican Republic
	"EC": ZoneLATAM, // Ecuador
	"FK": ZoneLATAM, // Falkland Islands
	"GD": ZoneLATAM, // Grenada
	"GP": ZoneLATAM, // Guadeloupe
	"GT": ZoneLATAM, // Guatemala
	"GY": ZoneLATAM, // Guyana
	"HN": ZoneLATAM, // Honduras
	"HT": ZoneLATAM, // Haiti
	"JM": ZoneLATAM, // Jamaica
	"KN": ZoneLATAM, // Saint Kitts and Nevis
	"KY": ZoneLATAM, // Cayman Islands
	"LC": ZoneLATAM, // Saint Lucia
	"MF": ZoneLATAM, // Saint Martin (French)
	"MQ": ZoneLATAM, // Martinique
	"MS": ZoneLATAM, // Montserrat
	"MX": ZoneLATAM, // Mexico
	"NI": ZoneLATAM, // Nicaragua
	"PA": ZoneLATAM, // Panama
	"PE": ZoneLATAM, // Peru
	"PM": ZoneLATAM, // Saint Pierre and Miquelon
	"PY": ZoneLATAM, // Paraguay
	"SR": ZoneLATAM, // Suriname
	"SV": ZoneLATAM, // El Salvador
	"SX": ZoneLATAM, // Sint Maarten
	"TC": ZoneLATAM, // Turks and Caicos Islands
	"TT": ZoneLATAM, // Trinidad and Tobago
	"UY": ZoneLATAM, // Uruguay
	"VC": ZoneLATAM, // Saint Vincent and the Grenadines
	"VE": ZoneLATAM, // Venezuela
	"VG": ZoneLATAM, // British Virgin Islands

	// ── Middle East + North Africa ─────────────────────────────────────────────
	"AE": ZoneMENA, // United Arab Emirates
	"BH": ZoneMENA, // Bahrain
	"DZ": ZoneMENA, // Algeria
	"EG": ZoneMENA, // Egypt
	"EH": ZoneMENA, // Western Sahara
	"IL": ZoneMENA, // Israel
	"IQ": ZoneMENA, // Iraq
	"IR": ZoneMENA, // Iran
	"JO": ZoneMENA, // Jordan
	"KW": ZoneMENA, // Kuwait
	"LB": ZoneMENA, // Lebanon
	"LY": ZoneMENA, // Libya
	"MA": ZoneMENA, // Morocco
	"MR": ZoneMENA, // Mauritania
	"OM": ZoneMENA, // Oman
	"PS": ZoneMENA, // Palestine
	"QA": ZoneMENA, // Qatar
	"SA": ZoneMENA, // Saudi Arabia
	"SD": ZoneMENA, // Sudan
	"SY": ZoneMENA, // Syria
	"TN": ZoneMENA, // Tunisia
	"YE": ZoneMENA, // Yemen

	// ── Sub-Saharan Africa ─────────────────────────────────────────────────────
	"AO": ZoneSSA, // Angola
	"BF": ZoneSSA, // Burkina Faso
	"BI": ZoneSSA, // Burundi
	"BJ": ZoneSSA, // Benin
	"BW": ZoneSSA, // Botswana
	"CD": ZoneSSA, // Democratic Republic of Congo
	"CF": ZoneSSA, // Central African Republic
	"CG": ZoneSSA, // Republic of Congo
	"CI": ZoneSSA, // Ivory Coast
	"CM": ZoneSSA, // Cameroon
	"CV": ZoneSSA, // Cape Verde
	"DJ": ZoneSSA, // Djibouti
	"ER": ZoneSSA, // Eritrea
	"ET": ZoneSSA, // Ethiopia
	"GA": ZoneSSA, // Gabon
	"GH": ZoneSSA, // Ghana
	"GM": ZoneSSA, // Gambia
	"GN": ZoneSSA, // Guinea
	"GQ": ZoneSSA, // Equatorial Guinea
	"GW": ZoneSSA, // Guinea-Bissau
	"KE": ZoneSSA, // Kenya
	"KM": ZoneSSA, // Comoros
	"LR": ZoneSSA, // Liberia
	"LS": ZoneSSA, // Lesotho
	"MG": ZoneSSA, // Madagascar
	"ML": ZoneSSA, // Mali
	"MU": ZoneSSA, // Mauritius
	"MW": ZoneSSA, // Malawi
	"MZ": ZoneSSA, // Mozambique
	"NA": ZoneSSA, // Namibia (note: "NA" is also the ZoneNA constant — different types, no clash)
	"NE": ZoneSSA, // Niger
	"NG": ZoneSSA, // Nigeria
	"RE": ZoneSSA, // Réunion
	"RW": ZoneSSA, // Rwanda
	"SC": ZoneSSA, // Seychelles
	"SL": ZoneSSA, // Sierra Leone
	"SN": ZoneSSA, // Senegal
	"SO": ZoneSSA, // Somalia
	"SS": ZoneSSA, // South Sudan
	"ST": ZoneSSA, // São Tomé and Príncipe
	"SZ": ZoneSSA, // Eswatini
	"TD": ZoneSSA, // Chad
	"TG": ZoneSSA, // Togo
	"TZ": ZoneSSA, // Tanzania
	"UG": ZoneSSA, // Uganda
	"YT": ZoneSSA, // Mayotte
	"ZA": ZoneSSA, // South Africa
	"ZM": ZoneSSA, // Zambia
	"ZW": ZoneSSA, // Zimbabwe
}
