<script setup>
import Phases from "./Phases.vue";
import { reactive } from "vue";

const state = reactive({
	phasesActive: 1,
	minCurrent: 6,
	maxCurrent: 16,
	chargeCurrent: 8,
	chargeCurrents: null,
});
</script>

<template>
	<Story :layout="{ type: 'grid', width: 300 }">
		<Variant title="one phase">
			<Phases v-bind="state" />
		</Variant>
		<Variant title="two phase">
			<Phases v-bind="state" :phasesActive="2" />
		</Variant>
		<Variant title="three phase">
			<Phases v-bind="state" :phasesActive="3" />
		</Variant>
		<Variant title="real currents">
			<Phases
				v-bind="state"
				:phasesActive="3"
				:chargeCurrent="13"
				:chargeCurrents="[11, 9, 12]"
			/>
		</Variant>
		<Variant title="one phase, more available">
			<Phases v-bind="state" :chargeCurrent="12" :chargeCurrents="[6, 0.2, 0]" />
		</Variant>
		<Variant title="two phases active">
			<Phases
				v-bind="state"
				:phasesActive="2"
				:chargeCurrent="16"
				:chargeCurrents="[16, 16, 0.3]"
			/>
		</Variant>
		<Variant title="asymetric phases">
			<Phases
				v-bind="state"
				:phasesActive="2"
				:chargeCurrent="16"
				:chargeCurrents="[8, 0.9, 14]"
			/>
		</Variant>
		<Variant title="only second phase">
			<Phases
				v-bind="state"
				:phasesActive="1"
				:chargeCurrent="13"
				:chargeCurrents="[0, 13, 0]"
			/>
		</Variant>
		<Variant title="mainly third phase">
			<Phases
				v-bind="state"
				:phasesActive="1"
				:chargeCurrent="10"
				:chargeCurrents="[0.007, 0.009, 5.945]"
				:minCurrent="6"
				:maxCurrent="20"
			/>
		</Variant>
	</Story>
</template>
