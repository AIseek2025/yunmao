import type { components } from "./generated-api";

export type User = components["schemas"]["User"];
export type Room = components["schemas"]["Room"];
export type RoomSubscription = components["schemas"]["RoomSubscription"];
export type FeedRequest = components["schemas"]["CreateFeedRequest"];
export type FeedResponse = components["schemas"]["FeedRequest"];
export type Wallet = components["schemas"]["Wallet"];
export type PrepayResponse = components["schemas"]["PrepayResponse"];
export type PrepayRequest = components["schemas"]["CreatePrepayRequest"];
export type ChatMessage = components["schemas"]["ChatMessage"];
export type IceServer = components["schemas"]["IceServer"];
export type IceServersResponse = components["schemas"]["IceServersResponse"];
export type Order = components["schemas"]["Order"];

export type AuthToken = components["schemas"]["LoginResponse"];
export type DeviceAuthToken = components["schemas"]["AuthToken"];
export type NewFeedResponse = components["schemas"]["FeedResponse"];
