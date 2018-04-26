package serve

import (
	"encoding/json"
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"gopkg.in/yaml.v2"

	"github.com/alvelcom/redoubt/api"
	"github.com/alvelcom/redoubt/config"
	inter "github.com/alvelcom/redoubt/interpolation"
	"github.com/alvelcom/redoubt/probes"
	"github.com/alvelcom/redoubt/producers"
)

var fs = flag.NewFlagSet("redoubt serve", flag.ExitOnError)
var (
	listenAddr = fs.String("listen", "0.0.0.0:2326",
		`Listen for incomming request there`)
	configFile = fs.String("config", "test.yaml",
		`Configuration file to use`)
)

type Policy struct {
	Name    string
	Verify  []probes.Probe
	Produce []producers.Producer
}

func Main(args []string) {
	fs.Parse(args)
	log.Print(*listenAddr)

	data, err := ioutil.ReadFile(*configFile)
	if err != nil {
		log.Fatal("Can't read config file: ", err)
	}

	var c config.Config
	err = yaml.UnmarshalStrict(data, &c)
	if err != nil {
		log.Fatal("Can't unmarshal config file: ", err)
	}

	log.Printf("Config: %+v", c)

	policies, err := castPolicies(c.Policies)
	if err != nil {
		log.Fatal("Can't initialize policies: ", err)
	}
	log.Printf("Policies: %#v", policies)

	http.Handle("/v1/harvest", &harvestHandler{policies})
	log.Fatal(http.ListenAndServe(*listenAddr, nil))
}

func castPolicies(ps []config.Policy) ([]Policy, error) {
	policies := []Policy{}
	for _, p := range ps {
		policy := Policy{
			Name:    p.Name,
			Verify:  []probes.Probe{},
			Produce: []producers.Producer{},
		}

		for _, probe := range p.Verify {
			probe, err := probes.New(probe)
			if err != nil {
				return nil, err
			}
			policy.Verify = append(policy.Verify, probe)
		}

		for _, producer := range p.Produce {
			producer, err := producers.New(producer)
			if err != nil {
				return nil, err
			}
			policy.Produce = append(policy.Produce, producer)
		}

		policies = append(policies, policy)
	}
	return policies, nil
}

// A bit of middleware sugar
func ReadJSON(r *http.Request, j interface{}) error {
	if r.Body == nil {
		return errors.New("read json: no body")
	}
	return json.NewDecoder(r.Body).Decode(j)
}

func WriteJSON(w http.ResponseWriter, j interface{}) {
	w.Header().Add("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(j); err != nil {
		log.Print("WriteJSON failed: ", err)
	}
}

type harvestHandler struct {
	policies []Policy
}

func printJSON(j interface{}) error {
	return json.NewEncoder(os.Stdout).Encode(j)
}

func (h *harvestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req api.Request
	if err := ReadJSON(r, &req); err != nil {
		log.Print("Bad request: ", err)
		WriteJSON(w, map[string]string{"error": "bad"})
		return
	}

	env := inter.Env{
		Machine: req.Machine,
		User:    req.User,
	}

	printJSON(env)

	var resp api.Response
	for _, policy := range h.policies {
		for _, producer := range policy.Produce {
			t, p, err := producer.Produce(env)
			if err != nil {
				WriteJSON(w, map[string]string{"error": err.Error()})
				return
			}
			resp.Tasks = append(resp.Tasks, t...)
			resp.Products = append(resp.Products, p...)
		}
	}
	WriteJSON(w, resp)
}
