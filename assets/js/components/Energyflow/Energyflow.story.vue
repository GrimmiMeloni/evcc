<script setup>
import Energyflow from "./Energyflow.vue";
</script>

<template>
	<Story>
		<Variant title="grid and pv">
			<Energyflow
				gridConfigured
				pvConfigured
				:pvPower="7300"
				:gridPower="-2300"
				:homePower="800"
				:loadpointsCompact="[
					{ power: 1000, icon: 'car', charging: true },
					{ power: 1000, icon: 'bike', charging: true },
					{ power: 2200, icon: 'car', charging: true },
				]"
				:tariffGrid="0.25"
				:tariffFeedIn="0.08"
				:tariffEffectivePrice="0.08"
				smartCostType="price"
				smartCostAvailable
				currency="EUR"
				siteTitle="Home"
				:pv="[{ power: 5000 }, { power: 2300 }]"
			/>
		</Variant>
		<Variant title="battery and grid">
			<Energyflow
				gridConfigured
				pvConfigured
				batteryConfigured
				:pvPower="0"
				:gridPower="1200"
				:homePower="2000"
				:batteryPower="800"
				:batterySoc="77"
				:tariffGrid="0.25"
				:tariffFeedIn="0.08"
				:tariffEffectivePrice="0.08"
				currency="EUR"
				siteTitle="Home"
				:battery="[
					{ soc: 44.999, capacity: 13.3, power: 350 },
					{ soc: 82.3331, capacity: 21, power: 450 },
				]"
			/>
		</Variant>
		<Variant title="battery charging">
			<Energyflow
				gridConfigured
				pvConfigured
				batteryConfigured
				:pvPower="5000"
				:gridPower="-1300"
				:homePower="800"
				:loadpointsCompact="[{ power: 1400, icon: 'car', charging: true }]"
				:batteryPower="-1500"
				:batterySoc="75"
				siteTitle="Home"
			/>
		</Variant>
		<Variant title="grid, pv and battery">
			<Energyflow
				gridConfigured
				pvConfigured
				batteryConfigured
				:pvPower="1000"
				:gridPower="700"
				:homePower="3300"
				:batteryPower="1500"
				:batterySoc="30"
				siteTitle="Home"
			/>
		</Variant>
		<Variant title="battery thresholds">
			<Energyflow
				gridConfigured
				pvConfigured
				batteryConfigured
				:pvPower="8700"
				:gridPower="-500"
				:loadpointsCompact="[
					{ power: 5000, icon: 'car', charging: true },
					{ power: 2500, icon: 'bus', charging: true },
				]"
				:batteryPower="-700"
				:batterySoc="95"
				siteTitle="Home"
			/>
		</Variant>
		<Variant title="pv thresholds">
			<Energyflow
				gridConfigured
				pvConfigured
				batteryConfigured
				:pvPower="300"
				:gridPower="6500"
				:homePower="1000"
				:loadpointsCompact="[
					{ power: 5000, icon: 'car', charging: true },
					{ power: 1600, icon: 'car', charging: true },
				]"
				:batteryPower="800"
				:batterySoc="76"
				siteTitle="Home"
			/>
		</Variant>
		<Variant title="grid only">
			<Energyflow
				gridConfigured
				pvConfigured
				batteryConfigured
				:pvPower="0"
				:gridPower="6500"
				:homePower="1000"
				:loadpointsCompact="[
					{ power: 5500, icon: 'car', charging: true },
					{ power: 0, icon: 'car', charging: false },
					{ power: 0, icon: 'car', charging: false },
					{ power: 0, icon: 'car', charging: false },
				]"
				:batteryPower="0"
				:batterySoc="0"
				siteTitle="Home"
			/>
		</Variant>
		<Variant title="low power">
			<Energyflow
				gridConfigured
				pvConfigured
				batteryConfigured
				:pvPower="700"
				:gridPower="-300"
				:homePower="300"
				:batteryPower="-100"
				:batterySoc="55"
				:tariffGrid="0.25"
				:tariffFeedIn="0.08"
				:tariffEffectivePrice="0.08"
				currency="EUR"
				siteTitle="Home"
			/>
		</Variant>
		<Variant title="co2">
			<Energyflow
				gridConfigured
				pvConfigured
				:pvPower="7300"
				:gridPower="-2300"
				:homePower="800"
				:loadpointsCompact="[
					{ power: 1000, icon: 'car', charging: true },
					{ power: 1000, icon: 'car', charging: true },
					{ power: 2200, icon: 'car', charging: true },
				]"
				:tariffGrid="0.25"
				:tariffFeedIn="0.08"
				:tariffEffectivePrice="0.08"
				:tariffCo2="723"
				:tariffEffectiveCo2="0"
				smartCostType="co2"
				smartCostAvailable
				currency="EUR"
				siteTitle="Home"
				:pv="[{ power: 5000 }, { power: 2300 }]"
			/>
		</Variant>
		<Variant title="unknown input">
			<Energyflow
				gridConfigured
				pvConfigured
				:pvPower="2000"
				:gridPower="-2000"
				:loadpointsCompact="[{ power: 1000, icon: 'car', charging: true }]"
			/>
		</Variant>
		<Variant title="unknown input fill">
			<Energyflow
				gridConfigured
				pvConfigured
				batteryConfigured
				:pvPower="500"
				:gridPower="0"
				:batteryPower="-1000"
				:loadpointsCompact="[]"
			/>
		</Variant>
		<Variant title="unknown output">
			<Energyflow
				gridConfigured
				pvConfigured
				:pvPower="3000"
				:gridPower="-1000"
				:loadpointsCompact="[{ power: 1700, icon: 'car', charging: true }]"
			/>
		</Variant>
		<Variant title="unknown output (&lt; 10%)">
			<Energyflow
				gridConfigured
				pvConfigured
				:pvPower="3000"
				:gridPower="-1000"
				:loadpointsCompact="[{ power: 1800, icon: 'car', charging: true }]"
			/>
		</Variant>
	</Story>
</template>
