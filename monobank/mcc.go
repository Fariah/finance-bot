package monobank

// mccCategories maps MCC (Merchant Category Code) codes to human-readable categories.
// The list covers the most common expense categories that a typical user encounters.
var mccCategories = map[int]string{
	// Groceries
	5411: "Groceries (supermarkets)",
	5412: "Groceries (stores)",
	5422: "Meat",
	5441: "Bakeries",
	5451: "Dairy products",
	5462: "Bakery",
	5499: "Groceries (other)",

	// Cafes, restaurants, food delivery
	5811: "Food services",
	5812: "Restaurants",
	5813: "Bars",
	5814: "Fast food",

	// Transport
	4111: "Public transport",
	4121: "Taxi",
	4131: "Bus transportation",
	4511: "Airlines",
	5541: "Gas station (fuel)",
	5542: "Gas station (self-service)",
	7523: "Parking",

	// Health
	5912: "Pharmacies",
	8011: "Doctors",
	8021: "Dentistry",
	8062: "Hospitals",

	// Clothing and household
	5611: "Men's clothing",
	5621: "Women's clothing",
	5631: "Accessories",
	5641: "Children's clothing",
	5651: "Family clothing",
	5661: "Shoes",
	5691: "Clothing (stores)",
	5732: "Electronics",
	5734: "Computer stores",
	5722: "Appliances",
	5200: "Hardware stores",
	5300: "Wholesale stores",

	// Entertainment and subscriptions
	5815: "Streaming/digital content",
	5816: "Digital games",
	5817: "Gaming subscriptions",
	5818: "Digital goods (other)",
	7832: "Movie theaters",
	7922: "Theaters/concerts",
	7995: "Gambling",
	5992: "Flowers",

	// Communication and utilities
	4812: "Mobile communication",
	4814: "Telecommunications",
	4816: "Computer networks/internet",
	4899: "Cable/satellite TV",
	4900: "Utilities",

	// Financial transactions
	6011: "Cash withdrawal (ATM)",
	6012: "Financial services",
	6051: "Transfers/crypto",
	6538: "Account top-up",
	6540: "Card top-up",

	// Beauty and personal services
	7230: "Hair salons/beauty salons",
	7298: "Spa",

	// Education
	8211: "Schools",
	8220: "Universities",
	8241: "Online education",
	8299: "Education (other)",

	// Travel
	7011: "Hotels",
	4722: "Travel agencies",

	// Transfers
	4829: "Card-to-card transfer (P2P)",

	// Miscellaneous stores and services
	5310: "Discount stores",
	5331: "Variety stores",
	5251: "Hardware/hobby stores",
	5262: "Online marketplaces",
	5931: "Secondhand/thrift stores",
	5977: "Cosmetics stores",
	7941: "Sports clubs/facilities",
	7399: "Business services",
	8999: "Professional services",
	9402: "Postal services",
}

// CategoryForMCC returns a human-readable category name by MCC code.
// If the code is unknown, returns "Other".
func CategoryForMCC(mcc int) string {
	if category, ok := mccCategories[mcc]; ok {
		return category
	}
	return "Other"
}
