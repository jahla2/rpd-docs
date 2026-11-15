cmake_minimum_required(VERSION 3.20)
project(rpd_native_engine LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

find_package(OpenCV QUIET COMPONENTS core imgcodecs imgproc photo)

add_executable(rpd-native-engine
    src/main.cpp
    src/image_processor.cpp
)

target_include_directories(rpd-native-engine PRIVATE include)

if(OpenCV_FOUND)
  target_compile_definitions(rpd-native-engine PRIVATE RPD_WITH_OPENCV=1)
  target_link_libraries(rpd-native-engine PRIVATE ${OpenCV_LIBS})
  target_include_directories(rpd-native-engine PRIVATE ${OpenCV_INCLUDE_DIRS})
  message(STATUS "RPD native engine: OpenCV preprocessing enabled")
else()
  message(WARNING "RPD native engine: OpenCV not found; image preprocessing will use safe passthrough fallback")
endif()

if (MSVC)
  target_compile_options(rpd-native-engine PRIVATE /W4)
else()
  target_compile_options(rpd-native-engine PRIVATE -Wall -Wextra -Wpedantic)
endif()
