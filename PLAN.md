# probe — PLAN.md

## Overview

`probe` discovers and documents API endpoints through two complementary strategies:

1. **Static analysis (`probe scan`)** — reads source code to discover all routes and extract schemas from type signatures, annotations, validation decorators, and documentation comments. Works with no running server. Gives you **completeness**.
2. **Traffic observation (`probe intercept`)** — acts as a transparent reverse proxy that observes real requests and responses, inferring schemas from actual data. Gives you **accuracy**.

Used together: `probe scan` seeds the full route inventory from source; `probe intercept` enriches each endpoint with real observed schemas as traffic flows through. A confidence score on each endpoint reflects its source — skeleton-only (scan), partially observed, or fully observed.

**Domain:** `probe.run`
**Repository:** `AgusRdz/probe`
**Language:** Go 1.24 (`net/http/httputil` + `modernc.org/sqlite`)
**Module:** `github.com/AgusRdz/probe`
**Pairs with:** `spec` — `probe` discovers undocumented APIs, `spec` mocks documented ones

---

## Goals

- **Scan source code** to discover all routes and extract schemas from type definitions — no running server required
- **Run as a transparent reverse proxy** to observe real traffic and enrich scanned schemas with actual data
- Support all major web frameworks across JS/TS, Python, Go, Java, .NET, Ruby, PHP, Rust, Kotlin
- Support REST/JSON, GraphQL, XML, form-encoded, and gRPC across both modes
- Build confidence in schema fields through observation frequency (required vs optional)
- Export accumulated knowledge as OpenAPI 3.x YAML/JSON or Postman collection
- Never modify actual traffic — pure observation only
- Cross-platform: Linux, macOS, Windows (amd64 + arm64)
- Self-updating binary with Ed25519 signature verification

## Non-Goals

- Not a record-and-replay tool (use `hookr` for that)
- No request modification or transformation
- No authentication flows simulation
- Not a load testing or stress tool
- No real-time schema validation against a reference spec

---

## CLI Interface

```
# Static analysis — no running server required
probe scan                                # scan current directory, auto-detect framework
probe scan --dir ./src                    # scan specific directory
probe scan --framework nestjs             # force framework (skip auto-detect)
probe scan --framework aspnet-mvc,aspnet-minimal  # multi-framework monorepo
probe scan --dry-run                      # print found endpoints without storing
probe scan --verbose                      # show which files matched which patterns

# Traffic observation — transparent proxy
probe intercept --target http://localhost:3001
probe intercept --target http://localhost:3001 --port 9000
probe intercept --target http://api.internal:8080
probe intercept --target http://localhost:3001 --filter /api
probe intercept --target http://localhost:3001 --ignore /health,/metrics
probe intercept --target http://localhost:3001 --grpc-reflect
probe intercept --target http://localhost:3001 --db ./custom.db

# Discovery / inspection
probe list                               # all endpoints (scan + observed)
probe list --json
probe list --source scan                 # only scanned (never observed)
probe list --source observed             # only traffic-observed
probe list --min-calls 5
probe list --protocol graphql

probe show GET /users                    # full detail: schema + confidence breakdown
probe show GET /users/{id}
probe show GET /users --calls
probe show "QUERY ListUsers"

# Export
probe export --format openapi
probe export --format openapi --out openapi.yaml
probe export --format postman --out collection.json
probe export --format openapi --min-confidence 0.8
probe export --format openapi --include-skeleton  # include scan-only endpoints

# Annotation / overrides
probe annotate "GET /users"
probe annotate "GET /users/{id}" --description "Get user by ID" --tag users
probe annotate "GET /users/{id}" --path-override "/users/me"

# Maintenance
probe stats
probe clear
probe clear --endpoint "GET /users"
probe config show
probe update
probe version
probe help [command]
```

### Output (`probe list`)

```
  METHOD  PATH                    SOURCE    CALLS  CONFIDENCE  PROTOCOL  STATUS CODES
  GET     /users                  observed  47     ████████ 94%  rest    200
  GET     /users/{id}             observed  23     ██████░░ 78%  rest    200, 404
  POST    /users                  observed  8      █████░░░ 61%  rest    201, 422
  DELETE  /users/{id}             scan+obs  3      ███░░░░░ 42%  rest    204, 404
  GET     /orders                 scan+obs  1      █░░░░░░░ 15%  rest    200
  PATCH   /users/{id}             scan      0      ░░░░░░░░  0%  rest    —        ← not yet seen
  GET     /admin/reports          scan      0      ░░░░░░░░  0%  rest    —        ← not yet seen
  QUERY   ListUsers               observed  12     ████████ 91%  graphql 200
  POST    /grpc.Svc/GetItem       observed  5      █████░░░ 60%  grpc    OK
```

**SOURCE column:**
- `scan` — found in source code only, never observed in traffic
- `observed` — discovered purely via proxy traffic
- `scan+obs` — found in source AND confirmed via traffic (highest trust)

---

## Architecture

### Package structure

```
probe/
├── main.go
├── cmd/
│   ├── root.go
│   ├── scan.go            # probe scan
│   ├── intercept.go       # probe intercept
│   ├── list.go
│   ├── show.go
│   ├── export.go
│   ├── annotate.go
│   ├── stats.go
│   ├── clear.go
│   ├── config.go
│   └── version.go
├── scanner/
│   ├── scanner.go         # orchestrator: detect language/framework, run extractors
│   ├── detect.go          # framework detection from project files
│   ├── types.go           # ScannedEndpoint, ExtractedParam, ExtractedSchema
│   ├── js/
│   │   ├── express.go
│   │   ├── fastify.go
│   │   ├── nestjs.go      # decorators + DTO type resolution
│   │   ├── nextjs.go      # file-based routes (pages/api + app router)
│   │   ├── hono.go
│   │   ├── trpc.go        # procedure definitions
│   │   └── koa.go
│   ├── python/
│   │   ├── fastapi.go     # decorators + Pydantic model resolution
│   │   ├── flask.go       # @app.route + Blueprint
│   │   ├── django.go      # urls.py + DRF ViewSet/Serializer
│   │   └── litestar.go
│   ├── go/
│   │   ├── stdlib.go      # net/http HandleFunc / Handle
│   │   ├── chi.go
│   │   ├── gin.go
│   │   ├── echo.go
│   │   ├── fiber.go
│   │   └── gorilla.go     # gorilla/mux
│   ├── dotnet/
│   │   ├── mvc.go         # controller + [Http*] action attributes
│   │   ├── minimal.go     # app.Map* minimal API
│   │   └── types.go       # C# record/class property extraction
│   ├── java/
│   │   ├── spring.go      # @*Mapping + @RequestBody/@ResponseBody
│   │   ├── jaxrs.go       # @Path + @GET/@POST etc.
│   │   └── types.go       # Java class/record field extraction
│   ├── ruby/
│   │   ├── rails.go       # routes.rb: resources, get, post, namespace, scope
│   │   └── sinatra.go
│   ├── php/
│   │   ├── laravel.go     # Route:: facade + attribute routing
│   │   └── symfony.go     # #[Route] attributes + YAML config
│   ├── rust/
│   │   ├── actix.go       # #[get], #[post] proc macros
│   │   └── axum.go        # .route() + Router::new()
│   └── kotlin/
│       └── ktor.go        # routing DSL: get("/path") { }
├── proxy/
│   ├── proxy.go
│   └── capture.go
├── observer/
│   ├── observer.go
│   ├── path.go
│   ├── schema.go
│   ├── schema_json.go
│   ├── schema_graphql.go
│   ├── schema_xml.go
│   ├── schema_form.go
│   ├── schema_grpc.go
│   └── confidence.go
├── store/
│   ├── store.go
│   ├── schema.go
│   └── path.go
├── export/
│   ├── openapi.go
│   └── postman.go
├── render/
│   ├── table.go
│   ├── detail.go
│   └── json.go
├── config/
│   └── config.go
├── updater/
│   └── updater.go
├── color.go
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── install.sh
├── install.ps1
├── public_key.pem
├── cliff.toml
├── CLAUDE.md
└── PLAN.md
```

---

## probe scan — static analysis

### Framework detection (`scanner/detect.go`)

Auto-detect by scanning project files, starting from `--dir` (default: cwd), walking up one level:

```
Indicator file              → Framework(s) detected
─────────────────────────────────────────────────────
package.json (express dep)  → express
package.json (fastify dep)  → fastify
package.json (@nestjs/core) → nestjs
package.json (next dep)     → nextjs
package.json (hono dep)     → hono
package.json (@trpc/server) → trpc
package.json (koa dep)      → koa
requirements.txt / pyproject.toml (fastapi) → fastapi
requirements.txt / pyproject.toml (flask)   → flask
requirements.txt / pyproject.toml (django)  → django
requirements.txt / pyproject.toml (litestar)→ litestar
go.mod (github.com/go-chi/chi)              → chi
go.mod (github.com/gin-gonic/gin)           → gin
go.mod (github.com/labstack/echo)           → echo
go.mod (github.com/gofiber/fiber)           → fiber
go.mod (github.com/gorilla/mux)             → gorilla
go.mod (no web framework)                   → go-stdlib
*.csproj / *.sln                            → aspnet-mvc + aspnet-minimal (both)
pom.xml / build.gradle (spring-boot dep)    → spring
pom.xml / build.gradle (javax.ws.rs)        → jaxrs
Gemfile (rails dep)                         → rails
Gemfile (sinatra dep)                       → sinatra
composer.json (laravel/framework)           → laravel
composer.json (symfony/framework-bundle)    → symfony
Cargo.toml (actix-web dep)                  → actix
Cargo.toml (axum dep)                       → axum
build.gradle.kts / settings.gradle (ktor)  → ktor
```

Multiple frameworks detected = multiple extractors run (monorepo support).
`--framework` flag overrides detection.

### Extraction approach per language

**Regex-based route discovery** for all frameworks (fast, no language runtime needed).
**Regex-based type resolution** for typed languages to extract request/response schemas.
**Go AST** (`go/ast`) for Go projects — more accurate, handles embedded structs and json tags.

Extractors never execute code — read-only file scanning only.

### Framework extractors

#### JavaScript / TypeScript

**Express** (`scanner/js/express.go`)
```
Route patterns:
  app.get('/path', handler)
  app.post('/path', handler)
  router.METHOD('/path', handler)
  app.use('/prefix', router)          ← prefix propagation

Type extraction:
  req.body typed as interface/type    → extract fields
  Zod schema: z.object({...})         → extract fields + types
  Joi schema: Joi.object({...})       → extract fields
  JsDoc @param {TypeName} req.body    → follow TypeName definition
  TypeScript: Request<P, ResB, ReqB>  → extract ReqB generic type
  No types available                  → skeleton only
```

**Fastify** (`scanner/js/fastify.go`)
```
Route patterns:
  fastify.get('/path', opts, handler)
  fastify.route({ method, url, schema })
  fastify.register(plugin, { prefix })

Type extraction:
  schema.body (JSON Schema inline)    → extract directly
  schema.response[200]                → extract directly
  TypeBox: Type.Object({...})         → extract fields
  Zod: z.object({...})               → extract fields
```

**NestJS** (`scanner/js/nestjs.go`)
```
Route patterns:
  @Controller('/prefix')
  @Get('/path'), @Post('/path'), @Put(), @Patch(), @Delete()
  Composed: prefix + method path

Type extraction (high quality):
  @Body() dto: CreateUserDto          → resolve CreateUserDto class
  @Param('id') id: string             → path param: {id}: string
  @Query('page') page: number         → query param
  DTO class properties + types        → full schema
  class-validator decorators:
    @IsEmail()                        → format: email
    @IsUUID()                         → format: uuid
    @IsOptional()                     → required: false
    @MinLength(n)                     → minLength: n
    @MaxLength(n)                     → maxLength: n
    @Min(n)/@Max(n)                   → minimum/maximum
    @IsArray()                        → type: array
  @ApiProperty() (Swagger decorator)  → description, example
  Return type annotation              → response schema
```

**Next.js** (`scanner/js/nextjs.go`)
```
File-based routes (pages/api/):
  pages/api/users.ts                  → GET+POST /api/users
  pages/api/users/[id].ts             → /api/users/{id}
  pages/api/users/[...slug].ts        → /api/users/{slug} (wildcard)

App Router (app/api/):
  app/api/users/route.ts              → export GET, POST functions → /api/users
  app/api/users/[id]/route.ts         → /api/users/{id}

Type extraction:
  NextRequest body typed              → extract type
  zod schema in handler               → extract
```

**Hono** (`scanner/js/hono.go`)
```
Route patterns:
  app.get('/path', (c) => ...)
  app.METHOD('/path', handler)
  new Hono().basePath('/prefix')

Type extraction:
  Zod validator middleware            → extract schema
  TypeScript generics on context      → extract
```

**tRPC** (`scanner/js/trpc.go`)
```
Procedure patterns:
  router.query('name', { ... })       → virtual GET /trpc/name
  router.mutation('name', { ... })    → virtual POST /trpc/name
  t.procedure.input(schema)           → input = request schema
  t.procedure.output(schema)          → output = response schema
  Zod input/output schemas            → full extraction
```

---

#### Python

**FastAPI** (`scanner/python/fastapi.go`) — best-in-class extraction
```
Route patterns:
  @app.get('/path')
  @router.post('/path', response_model=UserResponse)
  APIRouter(prefix='/prefix')         ← prefix propagation

Type extraction (excellent):
  async def endpoint(body: CreateUserRequest):
    → resolve CreateUserRequest Pydantic model
    → extract all fields + types + validators

  Pydantic v1 + v2 field extraction:
    name: str                         → type: string
    email: EmailStr                   → type: string, format: email
    age: int                          → type: integer
    tags: List[str]                   → type: array, items: string
    address: AddressModel             → recurse into AddressModel
    Optional[str]                     → required: false
    Field(default=None)               → required: false
    Field(min_length=1)               → minLength: 1
    Field(pattern=r'...')             → pattern: "..."
    Literal['a', 'b']                 → enum: [a, b]

  response_model=UserResponse         → response schema (same resolution)
  status_code=201                     → response code
  Docstring                           → operation description
```

**Flask** (`scanner/python/flask.go`)
```
Route patterns:
  @app.route('/path', methods=['GET', 'POST'])
  @blueprint.route('/path')
  Blueprint(url_prefix='/prefix')

Type extraction (limited):
  flask-pydantic @validate(body=Schema) → Pydantic resolution
  marshmallow schema                    → field extraction
  No schema                             → skeleton only
```

**Django REST Framework** (`scanner/python/django.go`)
```
Route patterns (urls.py):
  path('users/', UserListView.as_view())
  path('users/<int:pk>/', UserDetailView.as_view())
  router.register('users', UserViewSet)   → generates full CRUD routes

Type extraction:
  serializer_class = UserSerializer      → resolve UserSerializer
  Serializer fields: CharField, IntField → extract type info
  required=False                         → optional
  read_only=True                         → response only
  write_only=True                        → request only
```

---

#### Go

Go scanners use `go/ast` (stdlib) for accurate parsing.

**Chi** (`scanner/go/chi.go`)
```
Route patterns:
  r.Get("/path", handler)
  r.Post("/path", handler)
  r.Route("/prefix", func(r chi.Router) { ... })  ← nested routes
  r.Mount("/prefix", subRouter)

Type extraction (via go/ast):
  handler func signature:
    func(w http.ResponseWriter, r *http.Request)
  Decode into struct:
    json.NewDecoder(r.Body).Decode(&req)   → resolve req type
    → walk struct fields + json tags
  Response struct:
    json.NewEncoder(w).Encode(resp)        → resolve resp type
  Embedded structs                         → flatten fields
  omitempty json tag                       → required: false
```

Same pattern applies to `gin`, `echo`, `fiber`, `gorilla`, `go-stdlib`.

---

#### .NET / C#

**ASP.NET Core MVC** (`scanner/dotnet/mvc.go`)
```
Route patterns:
  [Route("api/[controller]")]
  [HttpGet("{id}")]
  [HttpPost]
  [HttpPut("{id}")]
  [HttpDelete("{id}")]
  [RoutePrefix] (legacy Web API)

Controller class name → base path (UsersController → /users)
Action name → method (GetById → GET, Create → POST, etc.)

Type extraction (good):
  public ActionResult<UserResponse> GetById(int id)
    → path param: {id}: integer
    → response: resolve UserResponse

  public ActionResult Create([FromBody] CreateUserRequest request)
    → request body: resolve CreateUserRequest

  C# class/record property extraction:
    public string Name { get; set; }          → type: string
    public int? Age { get; set; }             → type: integer, required: false
    public List<string> Tags { get; set; }   → type: array, items: string
    public AddressDto Address { get; set; }  → recurse
    [Required]                               → required: true
    [EmailAddress]                           → format: email
    [StringLength(max, MinimumLength=min)]   → minLength, maxLength
    [Range(min, max)]                        → minimum, maximum
    [RegularExpression(pattern)]             → pattern
    [JsonIgnore]                             → exclude field
    XML doc comment (<summary>)             → description

  Data annotations on model classes → validation constraints in schema
  Swagger [ProducesResponseType(typeof(T), 200)] → response schema + code
```

**ASP.NET Core Minimal API** (`scanner/dotnet/minimal.go`)
```
Route patterns:
  app.MapGet("/path", handler)
  app.MapPost("/path", handler)
  app.MapPut("/path", handler)
  app.MapDelete("/path", handler)
  app.MapGroup("/prefix").MapGet(...)  ← group prefix propagation

Type extraction:
  Lambda with typed params:
    app.MapPost("/users", (CreateUserRequest req) => ...)
    → resolve CreateUserRequest (same C# class extraction)
  Results.Ok<UserResponse>(...)        → response schema
  IResult return type                  → attempt to resolve generic
  [FromBody], [FromQuery], [FromRoute] → parameter location
```

---

#### Java

**Spring Boot** (`scanner/java/spring.go`)
```
Route patterns:
  @RestController
  @RequestMapping("/api/users")
  @GetMapping("/{id}")
  @PostMapping
  @PutMapping("/{id}")
  @DeleteMapping("/{id}")
  @PatchMapping("/{id}")

  Class-level @RequestMapping → prefix
  Method-level → combined path

Type extraction (good):
  @RequestBody CreateUserRequest request
    → resolve CreateUserRequest class fields

  ResponseEntity<UserResponse> getById(@PathVariable Long id)
    → path param: {id}: integer (Long)
    → response: resolve UserResponse

  Java class field extraction:
    private String name;              → type: string
    private Integer age;              → type: integer
    private Boolean active;           → type: boolean
    private List<String> tags;        → type: array, items: string
    private AddressDto address;       → recurse

  Bean Validation annotations:
    @NotNull                          → required: true
    @NotBlank                         → required: true, minLength: 1
    @Email                            → format: email
    @Size(min=1, max=100)             → minLength, maxLength
    @Min(0) / @Max(100)              → minimum, maximum
    @Pattern(regexp="...")            → pattern

  Javadoc                             → operation description
  @ApiResponse (Swagger/OpenAPI)      → response code + schema
  record types (Java 16+)             → constructor params = fields
```

**JAX-RS** (`scanner/java/jaxrs.go`)
```
  @Path("/users")
  @GET / @POST / @PUT / @DELETE / @PATCH
  @Consumes("application/json")
  @Produces("application/json")
  @PathParam("id") / @QueryParam / @FormParam
  Same Java class extraction as Spring
```

---

#### Ruby

**Rails** (`scanner/ruby/rails.go`)
```
routes.rb patterns:
  resources :users                    → GET /users, POST /users,
                                        GET /users/:id, PUT/PATCH /users/:id,
                                        DELETE /users/:id
  resources :users, only: [:index, :show]
  resource :profile                   → singular resource
  namespace :api do ... end           → prefix /api
  scope '/v1' do ... end             → prefix /v1
  get '/search', to: 'users#search'
  post '/auth/login', to: 'sessions#create'
  member { get :activate }           → GET /users/:id/activate
  collection { get :search }         → GET /users/search

Type extraction (limited — Ruby is dynamic):
  Strong parameters:
    params.require(:user).permit(:name, :email)
    → request fields: name (string), email (string)
  Serializer (active_model_serializers / blueprinter):
    attributes :id, :name, :email    → response fields
  No types                           → skeleton only
```

**Sinatra** (`scanner/ruby/sinatra.go`)
```
  get '/path' do ... end
  post '/path' do ... end
  Named params: get '/users/:id'     → {id}
  Splat: get '/files/*'              → wildcard
  Type extraction: skeleton only
```

---

#### PHP

**Laravel** (`scanner/php/laravel.go`)
```
routes/api.php patterns:
  Route::get('/path', [Controller::class, 'method'])
  Route::post('/path', handler)
  Route::apiResource('users', UserController::class)
    → generates index, store, show, update, destroy
  Route::group(['prefix' => 'v1'], function() { ... })
  Route::middleware('auth')->group(...)

Type extraction:
  FormRequest class:
    public function rules(): array {
      return ['name' => 'required|string', 'email' => 'required|email']
    }
    → extract field names + validation rules
  API Resource:
    return new UserResource($user)    → resolve UserResource::toArray()
    → extract response fields
  Docblock on controller method      → description
```

**Symfony** (`scanner/php/symfony.go`)
```
  #[Route('/path', methods: ['GET'])]   (PHP 8 attributes)
  @Route("/path", methods={"GET"})      (annotations)
  YAML/XML route config                 → parse route name + path + methods

Type extraction:
  #[MapRequestPayload] CreateUserDto   → resolve DTO class
  Symfony Form types                   → extract fields
  Serializer attributes                → response fields
```

---

#### Rust

**Actix-web** (`scanner/rust/actix.go`)
```
  #[get("/path")]
  #[post("/path")]
  #[put("/path")]
  #[delete("/path")]
  web::scope("/prefix")
  .service(web::resource("/path").route(web::get().to(handler)))

Type extraction:
  async fn handler(body: web::Json<CreateUser>) → resolve CreateUser struct
  Serde struct:
    #[derive(Deserialize, Serialize)]
    struct CreateUser { name: String, age: u32 }
    → name: string (required), age: integer (required)
    Option<T>                         → required: false
    #[serde(rename = "camelName")]    → field name override
    #[serde(skip_serializing_if)]     → required: false
    #[serde(skip)]                    → exclude field
    Validator crate annotations       → constraints
```

**Axum** (`scanner/rust/axum.go`)
```
  Router::new().route("/path", get(handler))
  Router::new().route("/path", post(handler).put(handler))
  Router::new().nest("/prefix", sub_router)

Type extraction:
  async fn handler(Json(body): Json<CreateUser>) → resolve CreateUser
  Same serde struct extraction as actix
  axum-valid / validator crate        → constraints
```

---

#### Kotlin

**Ktor** (`scanner/kotlin/ktor.go`)
```
  routing {
    get("/path") { ... }
    post("/path") { ... }
    route("/prefix") { get("/sub") { ... } }
  }

Type extraction:
  call.receive<CreateUserRequest>()   → resolve data class
  Kotlin data class:
    data class CreateUserRequest(
      val name: String,
      val email: String,
      val age: Int? = null
    )
    → name: string (required), email: string (required),
       age: integer (optional, has default)
  kotlinx.serialization @Serializable → field extraction
  call.respond(UserResponse(...))      → resolve response type
```

---

### Scan output (`probe scan --verbose`)

```
Scanning ./src...

  Detected: nestjs (package.json → @nestjs/core 10.x)

  src/users/users.controller.ts
    GET     /users                  schema: UserResponseDto (8 fields)
    GET     /users/{id}             schema: UserResponseDto (8 fields)
    POST    /users                  request: CreateUserDto (5 fields), response: UserResponseDto
    PATCH   /users/{id}             request: UpdateUserDto (4 fields, all optional)
    DELETE  /users/{id}             skeleton only (no response type annotation)

  src/orders/orders.controller.ts
    GET     /orders                 schema: OrderResponseDto (12 fields)
    POST    /orders                 request: CreateOrderDto (7 fields)
    GET     /orders/{id}            schema: OrderResponseDto (12 fields)

  src/auth/auth.controller.ts
    POST    /auth/login             request: LoginDto (2 fields), response: TokenDto (3 fields)
    POST    /auth/refresh           skeleton only
    DELETE  /auth/logout            skeleton only

  11 endpoints stored. Run `probe intercept` to enrich with observed schemas.
```

---

### ScannedEndpoint type (`scanner/types.go`)

```go
type ScannedEndpoint struct {
    Method       string
    PathPattern  string
    Protocol     string             // rest | graphql | grpc
    Framework    string             // express | nestjs | spring | aspnet-mvc | ...
    SourceFile   string             // path to file where route was found
    SourceLine   int
    ReqSchema    *Schema            // nil if not extractable
    RespSchema   *Schema            // nil if not extractable
    StatusCodes  []int              // from annotations (e.g. @ProducesResponseType)
    Description  string             // from docstring/JSDoc/XML comments
    Tags         []string           // from @ApiTags, group name, namespace
    Params       []ExtractedParam   // path + query params
    Deprecated   bool               // from @Deprecated / [Obsolete] / @deprecated
}

type ExtractedParam struct {
    Name     string
    In       string    // path | query | header
    Required bool
    Schema   Schema
}
```

---

### Confidence model

Confidence reflects data quality. The scale is designed so that traffic observation always beats static analysis alone.

```
Source                              Starting confidence
────────────────────────────────────────────────────────
scan (route only, no types)         5%
scan (route + partial schema)       20%
scan (route + full schema)          35%
scan+observed (1 call)              45%
scan+observed (3 calls)             60%
scan+observed (10 calls)            75%
observed only (10 calls)            70%
observed only (30+ calls)           85–100%
```

Export threshold applies to this combined score. Default: include all. `--min-confidence 0.5` would include only endpoints with at least partial traffic confirmation.

---

## Proxy + capture flow

```
probe intercept --target http://localhost:3001

  ┌─ client ─────────────────────────────────────────────────────┐
  │                                                                │
  │   POST http://localhost:4000/users                            │
  │                                        ┌── probe proxy ────┐ │
  │   ────────────────────────────────►    │  capture.Wrap()   │ │
  │                                        │  TeeReader ──────►│ │
  │                                        │  forward to       │ │
  │                                        │  localhost:3001   │ │
  │   ◄────────────────────────────────    │  capture resp     │ │
  │                                        │  ch ← Pair        │ │
  │                                        └──────────────────┘ │
  └────────────────────────────────────────────────────────────────┘
                           ↓ async
  drainer: CapturedPair → observer.Extract() → store.Record()
```

---

## Multi-protocol schema inference (intercept mode)

### Protocol detection

```
Content-Type: application/json + body has {"query":"..."}  → graphql
Content-Type: application/json                              → rest
Content-Type: application/graphql                          → graphql
Content-Type: application/x-www-form-urlencoded            → form
Content-Type: multipart/form-data                          → form
Content-Type: application/xml | text/xml                   → xml
Content-Type: application/grpc*                            → grpc
Unrecognized                                               → rest (try JSON, else binary)
```

### GraphQL
- Virtual path: `/graphql#OperationName` (or `#anonymous`)
- Infer schema from `data` key only; `errors` noted but not schema-inferred
- `probe list --protocol graphql` shows all operations

### XML
- stdlib `encoding/xml`; attributes → field with `xml_attr: true`
- Max depth: 20; namespace prefixes stripped by default

### Form-encoded / multipart
- All fields inferred as `{type: "string"}`; file fields as `{type: "string", format: "binary"}`

### gRPC
- Without `--grpc-reflect`: record path + status only
- With `--grpc-reflect`: full schema from proto descriptors via reflection API

---

## Path normalization

Normalization happens at read time, not write time. Raw paths stored in DB.

```
1. Pure integers                        → {id}
2. UUID (8-4-4-4-12 hex)              → {id}
3. ULID (26 chars, Crockford base32)  → {id}
4. CUID2 / NanoID (21+ alphanumeric)  → {id}
5. Slug with numeric suffix (abc-42)  → {id}
6. ALL-CAPS alphanumeric (ORD-9821)   → {id}
7. Cross-call confirmation (same position, seen as integer ≥ threshold) → {id}
8. Known keywords: me, self, current, latest, new, first, last, all, count, search → keep
9. Long strings / hash-like (>32 chars) → {id}
10. Manual override (.probe.yml)        → always wins
```

`probe list` shows `?` on patterns with unconfirmed segments (below threshold).

---

## Storage schema

```sql
CREATE TABLE IF NOT EXISTS endpoints (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    method          TEXT    NOT NULL,
    path_pattern    TEXT    NOT NULL,
    protocol        TEXT    NOT NULL DEFAULT 'rest',
    source          TEXT    NOT NULL DEFAULT 'observed', -- scan | observed | scan+obs
    framework       TEXT,                                -- nestjs | spring | aspnet-mvc | ...
    source_file     TEXT,                                -- from probe scan
    source_line     INTEGER,
    first_seen      TEXT    NOT NULL,
    last_seen       TEXT    NOT NULL,
    call_count      INTEGER NOT NULL DEFAULT 0,
    description     TEXT,
    tags_json       TEXT    NOT NULL DEFAULT '[]',
    deprecated      INTEGER NOT NULL DEFAULT 0,
    UNIQUE(method, path_pattern)
);

CREATE TABLE IF NOT EXISTS raw_paths (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint_id     INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    raw_path        TEXT    NOT NULL,
    seen_count      INTEGER NOT NULL DEFAULT 1,
    UNIQUE(endpoint_id, raw_path)
);

CREATE TABLE IF NOT EXISTS path_overrides (
    method          TEXT    NOT NULL,
    raw_prefix      TEXT    NOT NULL,
    override_pattern TEXT   NOT NULL,
    PRIMARY KEY (method, raw_prefix)
);

CREATE TABLE IF NOT EXISTS observations (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint_id     INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    observed_at     TEXT    NOT NULL,
    status_code     INTEGER NOT NULL,
    req_schema_json TEXT,
    resp_schema_json TEXT,
    req_content_type  TEXT,
    resp_content_type TEXT,
    latency_ms      INTEGER
);

CREATE TABLE IF NOT EXISTS field_confidence (
    endpoint_id     INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    location        TEXT    NOT NULL,   -- request | response
    field_path      TEXT    NOT NULL,   -- dot-notation: user.address.city
    seen_count      INTEGER NOT NULL DEFAULT 0,
    total_calls     INTEGER NOT NULL DEFAULT 0,
    type_json       TEXT    NOT NULL,
    PRIMARY KEY (endpoint_id, location, field_path)
);

CREATE INDEX IF NOT EXISTS idx_observations_endpoint   ON observations (endpoint_id);
CREATE INDEX IF NOT EXISTS idx_endpoints_method_path   ON endpoints (method, path_pattern);
CREATE INDEX IF NOT EXISTS idx_raw_paths_endpoint      ON raw_paths (endpoint_id);
CREATE INDEX IF NOT EXISTS idx_field_conf_endpoint_loc ON field_confidence (endpoint_id, location);
```

SQLite opened with:
```sql
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
```

DB path: `~/.local/share/probe/probe.db` (Linux/Mac), `%LOCALAPPDATA%\probe\probe.db` (Windows).
Override: `--db <path>` flag or `PROBE_DB` env var.

---

## Configuration

```yaml
# ~/.config/probe/config.yml  (global)
# .probe.yml                  (project — overrides global)

proxy:
  port: 4000
  bind: 127.0.0.1
  body_size_limit: 1048576       # 1MB

scan:
  dir: ./src                     # default scan root
  frameworks: []                 # empty = auto-detect
  exclude:
    - "**/node_modules/**"
    - "**/vendor/**"
    - "**/__pycache__/**"
    - "**/bin/**"
    - "**/obj/**"
    - "**/.git/**"

inference:
  path_normalization_threshold: 3
  confidence_threshold: 0.9
  max_xml_depth: 20
  xml_preserve_namespaces: false
  array_merge_items: true

export:
  default_format: openapi
  min_confidence: 0.0
  include_skeleton: false        # include scan-only (0 calls) endpoints
  openapi_version: "3.0.3"
  info_title: "Discovered API"
  info_version: "0.0.1"

output:
  no_color: false
  json_indent: 2

path_overrides:
  - pattern: "/api/v*/users/me"
    keep_as: "/api/v{version}/users/me"
```

---

## Export: OpenAPI 3.0.3

```
probe export --format openapi
  └── store.GetEndpoints()
        └── for each endpoint:
              ├── merge scan schema (if source=scan|scan+obs)
              ├── merge observed schema (field_confidence)
              ├── observed schema wins on conflicts
              ├── build PathItem:
              │     parameters   ← path/query params
              │     requestBody  ← merged req schema
              │     responses    ← per status code
              │     description  ← from scan or probe annotate
              │     tags         ← from scan or probe annotate
              │     deprecated   ← from scan
              └── merge into openapi.Paths
        └── render as YAML
```

- Scan-only endpoints included only with `--include-skeleton` flag (or `export.include_skeleton: true`)
- GraphQL → `POST /graphql` with `requestBody.operationName`
- gRPC → skipped unless `--include-grpc` flag and reflection data available
- Unconfirmed path patterns (`?`) excluded unless `--include-unconfirmed`

## Export: Postman Collection v2.1

- One folder per tag
- Request examples from highest-confidence observation (or scan schema if no observations)
- Pre-request script placeholder for auth

---

## Performance

- Proxy overhead: < 5ms per request
- `io.TeeReader`: body read once
- DB write async via buffered channel (size 100)
- `probe scan`: parallel file walking (one goroutine per framework extractor)
- `probe export`: single query + in-memory assembly
- Body cap: 1MB

---

## Security

- `--target` validated at startup: `http://` or `https://` only
- Strip `X-Forwarded-Host`, `X-Real-IP` before forwarding
- Default bind: `127.0.0.1`
- Field values never stored (schemas only)
- `probe scan` is read-only — never executes code, never writes to scanned directories
- SQLite DB: `0600` permissions
- Release binaries: Ed25519 + SHA256 verified before install

---

## Docker

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(VERSION)" -o probe ./main.go

FROM alpine:3.20
RUN addgroup -S probe && adduser -S probe -G probe
WORKDIR /home/probe
COPY --from=builder /src/probe /usr/local/bin/probe
USER probe
EXPOSE 4000
VOLUME ["/home/probe/.local/share/probe"]
ENTRYPOINT ["probe"]
```

---

## Makefile

```makefile
.PHONY: build install test lint clean cross release-patch release-minor

BINARY    := probe
VERSION   := $(shell git describe --tags --always --dirty)
LDFLAGS   := -ldflags "-s -w -X main.version=$(VERSION)"
BUILD_DIR := dist

build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./main.go

install: build
	cp $(BUILD_DIR)/$(BINARY) $(HOME)/.local/bin/$(BINARY)

test:
	go test ./... -race -timeout 60s

test-integration:
	go test ./... -tags=integration -timeout 120s

lint:
	go vet ./...
	staticcheck ./...

clean:
	rm -rf $(BUILD_DIR)

cross:
	GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-linux-amd64    ./main.go
	GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-linux-arm64    ./main.go
	GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-darwin-amd64   ./main.go
	GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-darwin-arm64   ./main.go
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe ./main.go

release-patch:
	git tag -a $(shell svu patch) -m "release $(shell svu patch)"
	git push origin $(shell svu patch)

release-minor:
	git tag -a $(shell svu minor) -m "release $(shell svu minor)"
	git push origin $(shell svu minor)

docker-build:
	docker build -t $(BINARY):$(VERSION) .
```

---

## Testing strategy

```
scanner/
  detect_test.go              — framework detection from fixture project files
  js/express_test.go          — route extraction from Express fixtures
  js/nestjs_test.go           — decorator parsing + DTO resolution
  js/nextjs_test.go           — file-based route discovery
  python/fastapi_test.go      — Pydantic model extraction
  python/django_test.go       — urls.py + DRF serializer extraction
  go/chi_test.go              — chi route + struct extraction (go/ast)
  dotnet/mvc_test.go          — [Http*] controller extraction + C# type resolution
  dotnet/minimal_test.go      — app.Map* extraction
  java/spring_test.go         — @*Mapping + Bean Validation extraction
  ruby/rails_test.go          — routes.rb resources + namespace parsing
  php/laravel_test.go         — Route:: facade + FormRequest rules
  rust/actix_test.go          — #[get] proc macro extraction + serde struct
  kotlin/ktor_test.go         — routing DSL + data class extraction

  testdata/
    express/app.js            — fixture Express app
    nestjs/users.controller.ts
    nextjs/pages/api/users/[id].ts
    fastapi/main.py
    spring/UserController.java
    aspnet-mvc/UsersController.cs
    aspnet-minimal/Program.cs
    rails/routes.rb
    laravel/routes/api.php
    ...

proxy/
  proxy_test.go               — response parity
  capture_test.go             — TeeReader doesn't mutate body

observer/
  path_test.go                — normalization table-driven
  schema_json_test.go         — JSON → Schema
  schema_graphql_test.go
  schema_xml_test.go
  schema_form_test.go
  schema_grpc_test.go
  confidence_test.go

store/
  store_test.go               — real SQLite :memory:
  schema_test.go              — migration idempotency

export/
  openapi_test.go             — golden files in testdata/
  postman_test.go             — golden files

Integration (-tags=integration):
  — scan + intercept round trip → export → validate OpenAPI
  — scan NestJS fixture → 11 endpoints → probe list --source scan
  — WAL concurrency: 10 readers + 1 writer
  — Large body truncation
  — GraphQL operation discovery
  — Scan-only export with --include-skeleton
```

**Rules:**
- TDD: failing test first
- Real SQLite (`:memory:`) — never mock the store
- `-race` always
- Golden files in `testdata/` for export + scanner tests
- Table-driven for all parsers, normalizers, extractors

---

## Dependencies

```
github.com/mattn/go-isatty       v0.0.20   — TTY detection
modernc.org/sqlite               latest    — CGO-free SQLite
gopkg.in/yaml.v3                 v3.0.1    — config + OpenAPI YAML output
google.golang.org/grpc           latest    — gRPC reflection (--grpc-reflect only)
google.golang.org/protobuf       latest    — proto descriptors (--grpc-reflect only)
```

No cobra, viper, zerolog, logrus, or HTTP frameworks.
`go/ast` used for Go scanner — stdlib, no external dep.
All other scanners use `regexp` + `bufio` — no external parsing libraries.

---

## Implementation order (phased)

### Phase 1 — Core proxy + REST/JSON intercept
1. `store/` — SQLite schema (with `source` column), WAL, migrations
2. `proxy/` + `capture/` — TeeReader, async channel, CapturedPair
3. `observer/schema_json.go` — JSON schema inference
4. `observer/path.go` — normalization rules, read-time computation
5. `observer/confidence.go` — field confidence scoring
6. `cmd/intercept.go` — wire everything
7. `cmd/list.go` + `render/table.go` (SOURCE column)
8. `cmd/show.go` + `render/detail.go`
9. `export/openapi.go`
10. `cmd/export.go`

### Phase 2 — Static analysis (scan)
11. `scanner/detect.go` — framework detection
12. `scanner/types.go` — ScannedEndpoint, ExtractedParam
13. `scanner/js/express.go` + `scanner/js/nestjs.go` (highest adoption)
14. `scanner/python/fastapi.go` (best type extraction)
15. `scanner/go/chi.go` + `scanner/go/gin.go` (go/ast)
16. `scanner/dotnet/mvc.go` + `scanner/dotnet/minimal.go`
17. `scanner/java/spring.go`
18. `cmd/scan.go` — wire scanner, store ScannedEndpoints
19. Remaining framework extractors (ruby, php, rust, kotlin, remaining js/python)

### Phase 3 — Additional protocols
20. `observer/schema_graphql.go`
21. `observer/schema_xml.go`
22. `observer/schema_form.go`
23. Protocol detection in `capture.go`

### Phase 4 — Polish + release tooling
24. `cmd/annotate.go` (description, tags, path overrides)
25. `export/postman.go`
26. `config/` — two-level YAML
27. `updater/` — Ed25519 self-update (from go-cli-boilerplate)
28. `color.go`, `render/json.go`
29. `cmd/stats.go`, `cmd/clear.go`, `cmd/config.go`
30. Makefile, Dockerfile, install scripts, GitHub Actions

### Phase 5 — gRPC (behind flag)
31. `observer/schema_grpc.go` + reflection client
32. `--grpc-reflect` in intercept command
