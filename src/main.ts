import "./styles/tokens.css";
import App from "./App.svelte";
import { mount } from "svelte";

const target = document.getElementById("app");
if (!target) throw new Error("ES Studio: #app mount point missing");
mount(App, { target });
