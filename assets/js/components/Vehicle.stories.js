import Vehicle from "./Vehicle.vue";
import i18n from "../i18n";

export default {
  title: "Main/Vehicle",
  component: Vehicle,
  argTypes: {},
};

const Template = (args, { argTypes }) => ({
  i18n,
  props: Object.keys(argTypes),
  components: { Vehicle },
  template: '<Vehicle v-bind="$props"></Vehicle>',
});

export const Base = Template.bind({});
Base.args = {
  vehicleTitle: "Mein Auto",
  enabled: true,
  connected: true,
  vehiclePresent: true,
  vehicleSoc: 42,
  targetSoC: 90,
};

export const Connected = Template.bind({});
Connected.args = {
  vehicleTitle: "Mein Auto",
  enabled: false,
  connected: true,
  vehiclePresent: true,
  charging: false,
  vehicleSoc: 66,
  targetSoC: 90,
};

export const ReadyToCharge = Template.bind({});
ReadyToCharge.args = {
  vehicleTitle: "Mein Auto",
  enabled: true,
  connected: true,
  vehiclePresent: true,
  charging: false,
  vehicleSoc: 66,
  targetSoC: 90,
};

export const Charging = Template.bind({});
Charging.args = {
  vehicleTitle: "Mein Auto",
  enabled: true,
  connected: true,
  vehiclePresent: true,
  charging: true,
  vehicleSoc: 66,
  targetSoC: 90,
};

const hoursFromNow = function (hours) {
  const now = new Date();
  now.setHours(now.getHours() + hours);
  return now.toISOString();
};

export const TargetChargePlanned = Template.bind({});
TargetChargePlanned.args = {
  vehicleTitle: "Mein Auto",
  enabled: false,
  connected: true,
  vehiclePresent: true,
  vehicleSoc: 31,
  minSoC: 20,
  charging: false,
  timerSet: true,
  timerActive: false,
  targetSoC: 45,
  targetTime: hoursFromNow(14),
};

export const TargetChargeActive = Template.bind({});
TargetChargeActive.args = {
  vehicleTitle: "Mein Auto",
  enabled: true,
  connected: true,
  vehiclePresent: true,
  vehicleSoc: 66,
  minSoC: 30,
  charging: true,
  timerSet: true,
  timerActive: true,
  targetSoC: 80,
  targetTime: hoursFromNow(2),
};

export const MinCharge = Template.bind({});
MinCharge.args = {
  vehicleTitle: "Mein Auto",
  enabled: true,
  connected: true,
  vehiclePresent: true,
  vehicleSoc: 17,
  minSoC: 20,
  charging: true,
  targetSoC: 90,
};

export const UnknownVehicleConnected = Template.bind({});
UnknownVehicleConnected.args = {
  vehicleTitle: "Mein Auto",
  enabled: false,
  connected: true,
  vehiclePresent: false,
};

export const UnknownVehicleReadyToCharge = Template.bind({});
UnknownVehicleReadyToCharge.args = {
  vehicleTitle: "Mein Auto",
  enabled: true,
  connected: true,
  vehiclePresent: false,
  charging: false,
};

export const UnknownVehicleCharging = Template.bind({});
UnknownVehicleCharging.args = {
  vehicleTitle: "Mein Auto",
  enabled: true,
  connected: true,
  vehiclePresent: false,
  charging: true,
};

export const Disconnected = Template.bind({});
Disconnected.args = {
  vehicleTitle: "Mein Auto",
  connected: false,
  vehiclePresent: false,
};

export const DisconnectedKnownSoc = Template.bind({});
DisconnectedKnownSoc.args = {
  vehicleTitle: "Mein Auto",
  connected: false,
  enabled: false,
  vehiclePresent: true,
  vehicleSoc: 17,
  targetSoC: 60,
};
