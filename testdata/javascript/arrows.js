const fetchData = () => {
  return true;
};
const makeThing = async function() {
  return fetchData();
};
const values = [1].map(() => 1);

class Service {
  run() {}
}

function outer() {
  function helper() {}
  return helper;
}
